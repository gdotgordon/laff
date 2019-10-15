// Package main runs the laff service.  It spins up an HTTP
// server to handle requests, which are processed by the api package.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gdotgordon/laff/api"
	"github.com/gdotgordon/laff/service"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

type cleanupTask func()

var (
	portNum  int    // listen port
	logLevel string // zap log level
	timeout  int    // server timeout in seconds
	cache    int    // length of cache
	workers  int    // number of cache worker goroutines
	limit    int    // rate limiter requests/second
)

func init() {
	flag.IntVar(&portNum, "port", 5000, "HTTP port number")
	flag.StringVar(&logLevel, "log", "production",
		"log level: 'production', 'development'")
	flag.IntVar(&timeout, "timeout", 30, "server timeout (seconds)")
	flag.IntVar(&cache, "cache", 10, "length of name and joke caches")
	flag.IntVar(&workers, "workers", 2, "number of cache worker goroutines")
	flag.IntVar(&limit, "limit", 10, "rate limiter requests/second")
}

func main() {
	flag.Parse()

	// We'll propagate the context with cancel thorughout the program,
	// to be used by various entities, such as http clients, server
	// methods we implement, and other loops using channels.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up logging.
	log, err := initLogging()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating logger: %v", err)
		os.Exit(1)
	}

	// Create the server to handle the IP verify service.  The API module will
	// set up the routes, as we don't need to know the details in the
	// main program.
	muxer := mux.NewRouter()

	// Build the service.
	svc, err := service.New(workers, cache, log)
	if err != nil {
		log.Errorf("error creating service", err)
		os.Exit(1)
	}
	go svc.RunCache(ctx)

	// Initialize the API layer.
	if err := api.Init(ctx, muxer, svc, limit, log); err != nil {
		log.Errorf("Error initializing API layer", "error", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Handler:      muxer,
		Addr:         fmt.Sprintf(":%d", portNum),
		ReadTimeout:  time.Duration(timeout) * time.Second,
		WriteTimeout: time.Duration(timeout) * time.Second,
	}

	// Start server
	go func() {
		log.Infow("Listening for connections", "port", portNum)
		if err := srv.ListenAndServe(); err != nil {
			log.Infow("Server completed", "err", err)
		}
	}()

	// Block until we shutdown.
	waitForShutdown(ctx, srv, log) //, service.Shutdown)
}

// Set up the logger, condsidering any env vars.
func initLogging() (*zap.SugaredLogger, error) {
	var lg *zap.Logger
	var err error

	pdl := strings.ToLower(os.Getenv("LAFF_LOG_LEVEL"))
	if strings.HasPrefix(pdl, "dev") {
		logLevel = "development"
	} else if strings.HasPrefix(logLevel, "dev") {
		logLevel = "development"
	} else {
		logLevel = "production"
	}

	var cfg zap.Config
	if logLevel == "development" {
		cfg = zap.NewDevelopmentConfig()
	} else {
		cfg = zap.NewProductionConfig()
	}
	cfg.DisableStacktrace = true
	lg, err = cfg.Build()
	if err != nil {
		return nil, err
	}
	return lg.Sugar(), nil
}

// Setup for clean shutdown with signal handlers/cancel.
func waitForShutdown(ctx context.Context, srv *http.Server,
	log *zap.SugaredLogger, tasks ...cleanupTask) {
	interruptChan := make(chan os.Signal, 1)
	signal.Notify(interruptChan, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// Block until we receive our signal.
	sig := <-interruptChan
	log.Debugw("Termination signal received", "signal", sig)
	for _, t := range tasks {
		t()
	}

	// Create a deadline to wait for.
	ctx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()
	srv.Shutdown(ctx)

	log.Infof("Shutting down")
}
