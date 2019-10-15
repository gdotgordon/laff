// Package api is the HTTP endpoint implementation for the laff service.
// This package deals with unmarshaling and marshaling payloads, dispatching
// to the service and processing those errors.
package api

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"

	tollboothV5 "github.com/didip/tollbooth/v5"
	"github.com/gdotgordon/laff/service"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// Definitions for the supported URL endpoints.
const (
	jokeURL   = "/v1/joke"
	statusURL = "/v1/status" // ping
)

// StatusResponse is the JSON returned for a liveness check as well as
// for other status notifications such errors.
type StatusResponse struct {
	Status string `json:"status"`
}

// API is the item that dispatches to the endpoint implementations.  It needs a
// reference to the laff service to be able to inoke the joke retrieval.
type apiImpl struct {
	svc *service.LaffService
	log *zap.SugaredLogger
}

// Init sets up the endpoint processing.  There is nothing returned, other
// than potntial errors, because the endpoint handling is configured in
// the passed-in muxer.
func Init(ctx context.Context, r *mux.Router, svc *service.LaffService, limit int, log *zap.SugaredLogger) error {
	ap := apiImpl{svc: svc, log: log}
	r.HandleFunc("/", ap.generateJoke).Methods(http.MethodGet)
	r.HandleFunc(jokeURL, ap.generateJoke).Methods(http.MethodGet)
	r.HandleFunc(statusURL, ap.getStatus).Methods(http.MethodGet)

	// As part of making the code "production-ready", we add a rate limiter to
	// the middleware chain.
	var limiterMiddleware = func(next http.Handler) http.Handler {
		return tollboothV5.LimitFuncHandler(tollboothV5.NewLimiter(float64(limit), nil),
			func(w http.ResponseWriter, r *http.Request) {
				rc := r.WithContext(ctx)
				next.ServeHTTP(w, rc)
			})
	}

	// Stick the context that contains the cancel into the request.
	var wrapContext = func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rc := r.WithContext(ctx)
			next.ServeHTTP(w, rc)
		})
	}

	// Log each request.
	var loggingMiddleware = func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Infow("Handling URL", "url", r.URL)
			next.ServeHTTP(w, r)
		})
	}
	r.Use(limiterMiddleware)
	r.Use(loggingMiddleware)
	r.Use(wrapContext)
	return nil
}

// generateJoke is the HTTP GET call invoked by the user.  It returns a
// plain text result, and works with utf-8 characters.
func (a *apiImpl) generateJoke(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		defer r.Body.Close()

		ioutil.ReadAll(r.Body)
	}
	msg, err := a.svc.Joke(r.Context())
	if err != nil {
		if _, ok := err.(service.RateLimitError); ok {
			a.writeErrorResponse(w, http.StatusTooManyRequests, err)
		} else {
			a.writeErrorResponse(w, http.StatusInternalServerError, err)
		}
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(msg + "\n"))
}

// Liveness check endpoint
func (a apiImpl) getStatus(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		defer r.Body.Close()

		ioutil.ReadAll(r.Body)
	}

	sr := StatusResponse{Status: "IP verify service is up and running"}
	b, err := json.MarshalIndent(sr, "", "  ")
	if err != nil {
		a.writeErrorResponse(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	w.Write(b)
}

// For HTTP bad request responses, serialize a JSON status message with
// the cause.
func (a apiImpl) writeErrorResponse(w http.ResponseWriter, code int, err error) {
	a.log.Errorw("invoke error", "error", err, "code", code)
	w.WriteHeader(code)
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	b, _ := json.MarshalIndent(StatusResponse{Status: err.Error()}, "", "  ")
	w.Write(b)
}
