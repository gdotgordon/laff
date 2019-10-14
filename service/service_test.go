package service

import (
	"context"
	"fmt"
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
