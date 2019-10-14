# laff
Get a chuckle from a random quip.

Fetches a new utf-8 name, fetches a joke, inserts said name into said joke.

Time spent: ~ 4 hours

Note: I already had boilerplate code for the main server launching session and HTTP handler layers (plus the docker files).  The bulk of the time was spent on the service implementation, and a good chunk of that was spent trying to work around the rate limiter for the name service that allows a maximum of something like 10-12 requests in 30 seconds.

## Introduction and Overview
The solution here implements a web service to retrieve nerdy Chuck Norris jokes tailored to a random name.  It accomplishes this by first fetching a first and last name from a name service, and then passing that name to another "Chuck Norris" joke service, which inserts the name into the joke.  That joke is returned to the user's browser as plain UTF-8 text.

It's actually a little more complicated than that, because for performance, I have implmented two caches, one for names, and one for completed jokes, using Go buffered channels.  There are background worker goroutines that fill the caches to capacity (and then block).  When a user invokes the joke API, the name and jokes are fetched from the channels, but if there is nothing in the channel (the channel read would block), the API fetches these items itself directly.  Note the addition of the background workers should not affect the performance of incoming requests, as these client requests will not have to do HTTP invocations if the items are cached.  In fact, it should be a speedup in that the caches are filled during periods of inactivity.


## Accessing and running the Laff Service program

All external packages are built with go modules, but then vendored, so the complete zip file is runnable.

Here are the steps (a Go toolchaion is required (I built with Go 1.13.1):
1. Unzip the zip file anywhere by running `unzip laff.zip`
2. cd to directory "laff"
3.Run `go build .`
4. Start the program by running `./laff`.  I recommend setting log to "dev" level (Uber zap logging) by running `./laff -log=dev`.  Note the default port is 5000, but the `-port` flag can be used to change that.  There are other configurable options that you can see with `./laff -help`.

Note in trying to determine whether the rate limiter issue was a platform issue, I also have a docker file and docker compose yaml, so if you want to run under Linux, you can also use `docker-compose up` and `docker-compose down` as an alternative.  I won't cover this approach further here, but it's been tested.

## Invoking the endpoints
There is really only one endpoint in the assignment, which can be invoked by a GET at the server address, for example with `curl http://localhost:5000`.  But to be more formal, this is the list of "standard" endpoints.
* 
* `/v1/status` **GET** a liveness status check
* `/v1/joke`   **GET** same as runniung the base url as above

## IMPORTANT NOTE!
The name service at http://uinames.com/api/ imposes *severe* rate limiting to the point where this program can handle only a restricted load.  The code was painstakingly written to be highly robust, concurrent, and scalable, but alas, the rate limiter on the name service kicks in with HTTP 429 and Retry-After response headers after about 10-12 calls in a minute.  I have put extensive debugging in my code, which indicates spurious invocations are not being done.  There is even a test case I wrote that does nothing but invoke the call, and it has the same limit, as do curl commands.  So the best I can do is present the code as written, which in my opinion, is a solid approach.

## Tests
There are a few unit tests in the service package, but they mostly test using the active endpoints.  If I had more time given the expected time to be spent, I would have added some table-driven tests that talked to a mock server, as well as a full-on integration test.

## The API

HTTP return codes:
* 200 (OK) for successful requests
* 500 (Internal Server Error) typically won't happen unless there is a system failure

### Architecture and Code Layout
The code has a main package which starts the HTTP server. This package creates a signal handler which is tied to a context cancel function. This allows for clean shutdown. The main code creates a service object. This service is then passed to the api layer, for use with the mux'ed incoming requests.

As mentioned, Uber Zap logging is used. In a real production product, I would have buried it in a logging interface.

Here is a more-specific roadmap of the packages:

### *api* package
Contains the HTTP handlers for the various endpoints. Primary responsibility is to unmarshal incoming requests, convert them to Go objects, and pass them off to the service layer, get the responses back from the service layer, convert any errors (or not) to appropriate HTTP status codes and send them back to the HTTP layer.  Note the external package `tollbooth` rate limiter is appied here.

### *service* package
The service implements the Laff service and is decoupled from the actual HTTP.  It provides a public API to get a joke, plus it implmentds internal methods to fetch from the name and joke services, as well as implmenting a cache on top of those two services.

## Architecture, Optimizations and Assumptions
There is one service method `Joke()` that handles the user requests.  There is an internal method to fetch the name via HTTP, and another one to fetch a joke, plugging in the retrieved name.  The `Joke()` method can use those directly when needed, but an important feature of the architecture is the name and joke caches.

### Caches



## External packages used

* github.com/didip/tollbooth - rate limiter middleware: MIT License
* github.com/gorilla/mux - HTTP muxer: BSD 3-Clause "New" or "Revised" License
* github.com/pkg/errors - improved error types: BSD 2-Clause "Simplified" License
* go.uber.org/zap (imports as go.uber.org/zap) - efficient logger: Uber license: https://github.com/uber-go/zap/blob/master/LICENSE.txt
