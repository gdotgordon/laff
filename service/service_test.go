package service

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestFetchJoke(t *testing.T) {
	svc, err := New(2, 5, newDebugLogger())
	if err != nil {
		t.Fatal("error creating service", err)
	}
	str, err := svc.fetchJoke(context.Background(),
		&NameResp{
			Name:    "Ryan",
			Surname: "Gonzalez",
			Gender:  "male",
			Region:  "United States"},
	)
	if err != nil {
		t.Fatal("error fetching joke", err)
	}
	fmt.Println("got joke:", str)
}

func TestFetchName(t *testing.T) {
	svc, err := New(1, 5, newDebugLogger())
	if err != nil {
		t.Fatal("error creating service", err)
	}
	name, err := svc.fetchName(context.Background())
	if err != nil {
		t.Fatal("error fetching joke", err)
	}
	fmt.Printf("got name:'%+v'\n", name)
}

func TestRunLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	svc, err := New(2, 10, newDebugLogger())
	if err != nil {
		t.Fatal("error creating service", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		svc.RunCache(ctx)
	}()
	time.Sleep(5 * time.Second)

	var lwg sync.WaitGroup
	for i := 0; i < 2; i++ {
		lwg.Add(1)
		go func() {
			defer lwg.Done()
			for i := 0; i < 3; i++ {
				str, err := svc.Joke(ctx)
				if err != nil {
					t.Fatalf("error reading joke: %v", err)
				}
				fmt.Printf("read joke: %s\n", str)
			}
		}()
	}
	lwg.Wait()
	cancel()
	wg.Wait()
	fmt.Println("name tries:", svc.nameTries)
}

// This test demonstrates that the rate limit occurs even in a simple sequence
// of HTTP invokes.  It removes all other program logic to eliniate the possibilty
// that an error somewhere else is causing the problem.
func TestNameService(t *testing.T) {
	client := http.DefaultClient
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Duration(rand.Intn(500))*time.Millisecond + 1)
			req, err := http.NewRequest("GET", nameURL, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Add("Accept", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}

			if resp.Body == nil {
				t.Fatal(errors.New("unexpected empty body"))
			}

			_, err = ioutil.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {

				// Workaround for the regretful state of the rate limiter for the
				// name service.
				if resp.StatusCode == http.StatusTooManyRequests {
					t.Fatal("rate limit error")
				}

				invErr := fmt.Errorf("invoking name fetch got HTTP status %d (%s)",
					resp.StatusCode, http.StatusText(resp.StatusCode))
				t.Fatal(invErr)
			}
		}()
	}
	wg.Wait()
	fmt.Println("done")
}

func newDebugLogger() *zap.SugaredLogger {
	config := zap.NewDevelopmentConfig()
	lg, _ := config.Build()
	return lg.Sugar()
}

func newNoopLogger() *zap.SugaredLogger {
	config := zap.NewProductionConfig()
	config.OutputPaths = []string{"/dev/null"}
	lg, _ := config.Build()
	return lg.Sugar()
}
