package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

// The unit tests use a test HTTP server with mock name and joke services.
// This mock service eliminates the dodgy actual rate limited name service and
// allows us to verify the correctness of the design and functionality.

// TestFetchJoke fetches a single joke from the mock service.
func TestFetchJoke(t *testing.T) {
	svc, err := New(2, 5, newNoopLogger())
	if err != nil {
		t.Fatal("error creating service", err)
	}

	tstSrv := NewTestServer()
	svc.nameURL = tstSrv.srv.URL + "/name"
	svc.jokeURL = tstSrv.srv.URL + "/jokes?"
	defer tstSrv.Shutdown()

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

	exp := "Ryan Gonzalez made joke 0"
	if str != exp {
		t.Fatal("Expected joke:", exp, ", got:", str)
	}
}

// TestFetchName fetches a single name from the mock service.
func TestFetchName(t *testing.T) {
	svc, err := New(2, 5, newNoopLogger())
	if err != nil {
		t.Fatal("error creating service", err)
	}

	tstSrv := NewTestServer()
	svc.nameURL = tstSrv.srv.URL + "/name"
	svc.jokeURL = tstSrv.srv.URL + "/jokes?"
	defer tstSrv.Shutdown()

	name, err := svc.fetchName(context.Background())
	if err != nil {
		t.Fatal("error fetching joke", err)
	}
	expName := "Name0"
	if name.Name != expName {
		t.Fatal("Expected name:", expName, ", got:", name.Name)
	}
	expSurname := "Surname0"
	if name.Surname != expSurname {
		t.Fatal("Expected name:", expSurname, ", got:", name.Surname)
	}
}

// TestRunLoop tests the overall server logic using mock name and joke services.
// TODO - analyze the names produced to ensure every name and joke in the sequence
// numbers are accounted for.
// TODO - negative test cases.
// Each joke returned is like Name# Surname# told joke 0 (there is no guarantee the
// name and joke sequence numbers are the same due to random ordering of goroutine
// execution and there being separate channels for name and joke items.
func TestRunLoop(t *testing.T) {
	svc, err := New(3, 10, newNoopLogger())
	if err != nil {
		t.Fatal("error creating service", err)
	}
	tstSrv := NewTestServer()
	svc.nameURL = tstSrv.srv.URL + "/name"
	svc.jokeURL = tstSrv.srv.URL + "/jokes?"
	defer tstSrv.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		svc.RunCache(ctx)
	}()

	// Allow the cache to do some (but not all) population.
	time.Sleep(2 * time.Second)

	jokes := make([]string, 0, 30)
	var mu sync.Mutex
	var lwg sync.WaitGroup
	for i := 0; i < 10; i++ {
		lwg.Add(1)
		go func() {
			defer lwg.Done()
			for i := 0; i < 3; i++ {
				str, err := svc.Joke(ctx)
				if err != nil {
					t.Errorf("error reading joke: %v", err)
				}
				fmt.Printf("read joke: %s\n", str)
				mu.Lock()
				jokes = append(jokes, str)
				mu.Unlock()
			}
		}()
	}
	lwg.Wait()
	cancel()
	wg.Wait()

	// Again, there is no guaranetee the sequence numbers in the name and joke match,
	// in fact, we observe they don't.
	var jokeExp = regexp.MustCompile(`^Name\d.* Surname\d.* made joke \d.*$`)
	for _, joke := range jokes {
		if !jokeExp.Match([]byte(joke)) {
			t.Fatalf("'%s' did not match regexp", joke)
		}
	}
}

// TestNameJokeServer has mock name and joke generator services.  By using this,
// we can verify the correctness of the code by avoiding the rate limiter issue.
type TestNameJokeServer struct {
	srv      *httptest.Server
	nextName int
	nextJoke int
	sync.Mutex
}

func NewTestServer() *TestNameJokeServer {
	ts := &TestNameJokeServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			io.ReadAll(r.Body)
			r.Body.Close()
		}
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		urlStr := r.URL.String()
		if strings.HasSuffix(urlStr, "/name") {
			ts.Lock()
			val := ts.nextName
			ts.nextName++
			ts.Unlock()
			name := NameResp{
				Name:    fmt.Sprintf("Name%d", val),
				Surname: fmt.Sprintf("Surname%d", val),
			}

			b, err := json.Marshal(name)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=UTF-8")
			w.Write(b)
			return
		}

		if strings.Contains(urlStr, "/jokes?") {
			values := r.URL.Query()
			fn := values.Get("firstName")
			ln := values.Get("lastName")
			if fn == "" || ln == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			ts.Lock()
			val := ts.nextJoke
			ts.nextJoke++
			ts.Unlock()

			jv := JokeValue{
				ID:         val,
				Joke:       fmt.Sprintf("%s %s made joke %d", fn, ln, val),
				Categories: []string{"nerdy"},
			}
			joke := JokeResp{
				Type:  "success",
				Value: jv,
			}
			b, err := json.Marshal(joke)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=UTF-8")
			w.Write(b)
			return
		}

		w.WriteHeader(http.StatusBadRequest)

	}))

	ts.srv = srv
	return ts
}

func (ts *TestNameJokeServer) Shutdown() {
	ts.srv.Close()
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
