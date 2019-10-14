// Package service implments the service that gets the output of the name
// service, and sends that to the joke service, leading finally to a
// "personalized" Chuck Norris joke.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	pkgerr "github.com/pkg/errors"
	"go.uber.org/zap"
)

const (
	nameURL = "http://uinames.com/api/"
	jokeURL = "http://api.icndb.com/jokes/random?"

	maxErrs = 50 // if the cache is erroring out consistently, shut it down.

	dfltRetry = 90 // wait this many seconds to retry if retry header not parsed
)

// rateLimitError signifies an HTTP 429 (too many requests) occurred, due
// to the stingy limit of the name service.  We capture the value of the
// retry wait from the HTTP Retry-After response header and delay that
// amnount of time, plus some slop.
type rateLimitError struct {
	retry int
}

func (rle rateLimitError) Error() string {
	return fmt.Sprintf("rate limit exceeded, retry in %d seconds", rle.retry)
}

// LaffService is the implmentation of the service that returns the jokes.
type LaffService struct {
	client     *http.Client
	nameChan   chan *NameResp
	jokeChan   chan string
	numWorkers int
	bufLen     int
	nameErrs   int
	jokeErrs   int
	log        *zap.SugaredLogger
	nameTries  int // Verification that we're not trying to fetch the name too often
}

// NameResp is to unmarshall the lookup of the name.
type NameResp struct {
	Name    string `json:"name"`
	Surname string `json:"surname"`
	Gender  string `json:"gender"`
	Region  string `json:"region"`
}

func (nr NameResp) String() string {
	return fmt.Sprintf("Name: first: %s,, last: %s", nr.Name, nr.Surname)
}

// JokeResp is to unmarshall the lookup of the joke.
type JokeResp struct {
	Type  string    `json:"type"`
	Value JokeValue `json:"value"`
}

// JokeValue is the nested part of the joke response.
type JokeValue struct {
	ID         int      `json:"id"`
	Joke       string   `json:"joke"`
	Categories []string `json:"categories,omitempty"`
}

// New creates a new LaffService, which both runs the workers to populate
// the name and joke buffers, plus offers a public API to get the joke
// with the name inserted.
func New(numWorkers, bufLen int, logger *zap.SugaredLogger) (*LaffService, error) {
	// Customize the Transport to have larger connection pool
	defaultRoundTripper := http.DefaultTransport
	defaultTransportPointer, ok := defaultRoundTripper.(*http.Transport)
	if !ok {
		panic(fmt.Sprintf("defaultRoundTripper not an *http.Transport"))
	}

	// Increase the number of pooled connections per host (the default is 2).
	defaultTransport := *defaultTransportPointer // dereference it to get a copy of the struct that the pointer points to
	defaultTransport.MaxIdleConns = 100
	defaultTransport.MaxIdleConnsPerHost = 100

	c := &http.Client{Transport: &defaultTransport}
	ls := LaffService{
		client:     c,
		nameChan:   make(chan *NameResp, bufLen),
		jokeChan:   make(chan string, bufLen),
		numWorkers: numWorkers,
		bufLen:     bufLen,
		log:        logger,
	}
	return &ls, nil
}

// RunCache is the function that adds jokes to the buffered channel, so that
// jokes can be pre-built when the user calls in.
func (ls *LaffService) RunCache(ctx context.Context) {
	var wg sync.WaitGroup

	// Due to the name service rate limiter shutting us down, we'll sleep in
	// between name accesses such that we do at most 6 accesses/minute total
	// among all goroutines.
	sleepInterval := time.Duration((60 / (6 / ls.numWorkers))) * time.Second

	for i := 0; i < ls.numWorkers; i++ {
		// Capture loop index so each goruotine has correct value.
		i := i

		// Goroutine that fetches names and writes them to the name channel cache.
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
			Loop:
				// First try to get a name from the service.
				name, err := ls.fetchName(ctx)
				if err != nil {
					// If we got an error, handle a rate limit error
					// with a long delay.  For all other errors, increment
					// the total error count.
					switch v := err.(type) {
					case rateLimitError:
						ls.log.Errorw("Fetch name rate limit error",
							"goroutine", i, "error", err)
						ticker := time.NewTicker(time.Duration(v.retry+5) * time.Second)
						select {
						case <-ctx.Done():
							ticker.Stop()
							return
						case <-ticker.C:
							ticker.Stop()
							goto Loop
						}
					default:
						ls.log.Errorw("Fetch name error",
							"goroutine", i, "error", err)
						ls.nameErrs++
						if ls.nameErrs == maxErrs {
							ls.log.Errorw("Too many errors on name fetch, shutting cache",
								"count", maxErrs)
							return
						}
						goto Loop
					}
				}

				// Write the name to the channel cache when not blocked.
				select {
				case <-ctx.Done():
					return
				case ls.nameChan <- name:
					ls.log.Debugw("Wrote name to channel", "gorouitne", i, "name", name)
				}

				// Calculated delay due to rate limiter.
				{
					ticker := time.NewTicker(sleepInterval)
					select {
					case <-ctx.Done():
						ticker.Stop()
						return
					case <-ticker.C:
						ticker.Stop()
					}
				}
			}
		}()

		// Goroutine that reads names from the name cache, gets a joke and composes
		// the final joke and writes that to the joke channel cache.
		wg.Add(1)
		go func() {
			defer wg.Done()

			var name *NameResp
			var err error
			for {
				select {
				case <-ctx.Done():
					return
				case name = <-ls.nameChan:
					ls.log.Debugw("Read name from channel", "gorouitne", i, "name", name)
				}

				var joke string
				for {
					if joke, err = ls.fetchJoke(ctx, name); err != nil {
						ls.log.Errorw("Fetch joke error", "gorouitne", i, "error", err)
						fmt.Println(i, ": fetch joke error", err)
						ls.jokeErrs++
						if ls.nameErrs == maxErrs {
							return
						}
						continue
					}
					break
				}
				select {
				case <-ctx.Done():
					return
				case ls.jokeChan <- joke:
					ls.log.Debugw("Wrote joke to channel", "gorouitne", i, "joke", joke)
				}
			}
		}()
	}

	wg.Wait()
	ls.log.Debugw("cache done, returning.")
}

// Joke is the function invoked from the user's HTTP request.  It attempts
// to pull a joke out of the channel (cache) first.  If there is nothing
// in the joke cache, it then tries to pull a name from the name cache, and
// use that to invoke the joke fetch.  If the name cache is also empty, then
// the call simply makes the HTTP calls to fetch the name, and uses that name
// to plug into the joke fetch HTTP call.
func (ls *LaffService) Joke(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		// Cancel was invoked.
		return "", context.Canceled
	case jk := <-ls.jokeChan:
		// A joke is available in the joke cache.
		ls.log.Debugw("Got joke from channel", "joke", jk)
		return jk, nil
	default:
		// Joke is not available from the cache.
		select {
		case nm := <-ls.nameChan:
			// Got the next name from the cache.
			return ls.fetchJoke(ctx, nm)
		default:
			// Nothing in the name cache, so fetch the name and cache directly.
			ls.log.Debugw("Fetch name and joke directly")
			name, err := ls.fetchName(ctx)
			if err != nil {
				return "", err
			}
			return ls.fetchJoke(ctx, name)
		}
	}
}

// fetchName invokes the HTTP call to get a name repsonse.
func (ls *LaffService) fetchName(ctx context.Context) (*NameResp, error) {
	req, err := http.NewRequest("GET", nameURL, nil)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	req.Header.Add("Accept", "application/json")
	ls.nameTries++
	resp, err := ls.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.Body == nil {
		ls.log.Errorw("empty body for name fetch")
		return nil, errors.New("unexpected empty body")
	}

	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {

		// Workaround for the regretful state of the rate limiter for the
		// name service.
		if resp.StatusCode == http.StatusTooManyRequests {
			retry := resp.Header.Get("Retry-After")
			ls.log.Debugw("rate limit", "retry after", retry)
			var delay int = dfltRetry
			v, err := strconv.Atoi(retry)
			if err == nil {
				delay = v
			}
			return nil, rateLimitError{retry: delay}
		}

		invErr := fmt.Errorf("invoking name fetch got HTTP status %d (%s)",
			resp.StatusCode, http.StatusText(resp.StatusCode))
		ls.log.Errorw("Fetch name error", "error", invErr)
		return nil, invErr

	}

	// The call succeeded, so unmarshal the response.
	var nameResp NameResp
	if err := json.Unmarshal(b, &nameResp); err != nil {
		ls.log.Errorw("Fetch name json unmarshal error", "error", err)
		return nil, pkgerr.Wrap(err, "unmarshaling request body")
	}
	return &nameResp, nil
}

// fetchJoke fetches a joke, given a first and last name.
func (ls *LaffService) fetchJoke(ctx context.Context, name *NameResp) (string, error) {
	invURL := encodeJokeURL(name.Name, name.Surname)
	req, err := http.NewRequest("GET", invURL, nil)
	if err != nil {
		return "", err
	}
	req = req.WithContext(ctx)
	req.Header.Add("Accept", "application/json")
	resp, err := ls.client.Do(req)
	if err != nil {
		return "", err
	}
	if resp.Body == nil {
		ls.log.Errorw("empty body for joke fetch")
		return "", errors.New("unexpected empty body")
	}

	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		invErr := fmt.Errorf("invoking joke fetch got HTTP status %d (%s)",
			resp.StatusCode, http.StatusText(resp.StatusCode))
		ls.log.Errorw("Fetch joke error", "error", invErr)
		return "", invErr

	}

	// The call succeeded, so unmarshal the response.
	var jokeResp JokeResp
	if err := json.Unmarshal(b, &jokeResp); err != nil {
		ls.log.Errorw("Fetch joke json unmarshal error", "error", err)
		return "", pkgerr.Wrap(err, "unmarshaling request body")
	}
	return jokeResp.Value.Joke, nil
}

// encodeJokeURL escapes the query paramerters.  This is important
// as a name could contain a character that needs escaping.
func encodeJokeURL(firstName, lastName string) string {
	jurl, err := url.Parse(jokeURL)
	if err != nil {
		panic("invalid joke url")
	}

	parameters := url.Values{}
	parameters.Set("firstName", firstName)
	parameters.Set("lastName", lastName)
	parameters.Add("limitTo", "nerdy")
	jurl.RawQuery = parameters.Encode()
	return jurl.String()
}
