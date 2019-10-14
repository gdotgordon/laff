# laff
Get a chuckle from a random quip.

Fetches a new utf-8 name, fetches a joke, inserts said name into said joke.

## Introduction and Overview
The solution here implments a web service to retrieve nerdy Chuck Norris jokes tailored to a random name.  It accomplishes this by first fetching a first and last name from a name service, and then passing that name to antoher "Chuck Norris" joke service, which inserts the name into the joke.  That joke is returned to the user's browser as plain UTF-8 text.

It's actually a little more complicated than that, because for performance, I have implmented two caches, one for names, and one for completed jokes, using Go buffered channels.  There a background workers that fill the caches to capacity (and then block).  Thus when a user invokes the API, the name and jokes are fetched from the channels, and if there is nothing in the channel, the API fetches these items itlsef directly.  Note the addition of the background workers should not affect the performance of incoming requests, as these requests will not have to do HTTP invocations if the items are cached.  In fact, it should be a speedup


## Accessing and running the demo

All external packages are built with go modules, but then vendored, so the complete zip file is runnable.

To summarize, here are the steps:

## IMPORTANT NOTE!

The name service at http://uinames.com/api/ imposes *severe* rate limiting to the point where this program can handle only a restricted load.  The code was painstakingly written to be highly robust, concurrent, and scalable, but alas, the rate limiter on the name service kicks in with HTTP 429 and Retry-After response headers after about 10-12 calls in a minute.  I have put extensive debugging in my code, which indicates spurious invocations are not being done.  So the best I can do is present the code as written, which in my opinion, is a solid approach.

## Tests
There are a few unit tests in the service package, but they mostly test using the active endpoints.  If I had more time given the expected time to be spent, I would have added some table-driven tests that talked to a mock server, as well as a full-on integration test.

## Key Items and Artifacts and How To Run the Laff Service
There is really only one endpoint in the assignment, which can be invoked by a GET at the server address, for example with `curl http://localhost:8080/`
* 
* `/v1/status` **GET** a liveness status check
* `/v1/joke`   **GET** same as runniung the base url as above

## The API

HTTP return codes:
* 200 (OK) for successful requests
* 500 (Internal Server Error) typically won't happen unless there is a system failure

### Architecture and Code Layout
The code has a main package which starts the HTTP server. This package creates a signal handler which is tied to a context cancel function. This allows for clean shutdown. The main code creates a service object, which is a wrapper around the store package, which uses the sqlite3 database. This service is then passed to the api layer, for use with the mux'ed incoming requests.

As mentioned, Uber Zap logging is used. In a real production product, I would have buried it in a logging interface.

Here is a more-specific roadmap of the packages:

### *api* package
Contains the HTTP handlers for the various endpoints. Primary responsibility is to unmarshal incoming requests, convert them to Go objects, and pass them off to the service layer, get the responses back from the service layer, convert any errors (or not) to appropriate HTTP status codes and send them back to the HTTP layer.  Note the external package `tollbooth` rate limiter is appied here.

### *service* package
The service implements the Laff service and is decoupled from the actual HTTP.  It provides a public API to get a joke, plus it implmentds internal methods to fetch from the name and joke services, as well as implmenting a cache on top of those two services.

## Architecture, Optimizations and Assumptions


## External packages used

* github.com/didip/tollbooth - rate limiter middleware: MIT License
* github.com/gorilla/mux - HTTP muxer: BSD 3-Clause "New" or "Revised" License
* github.com/pkg/errors - improved error types: BSD 2-Clause "Simplified" License
* go.uber.org/zap (imports as go.uber.org/zap) - efficient logger: Uber license: https://github.com/uber-go/zap/blob/master/LICENSE.txt
