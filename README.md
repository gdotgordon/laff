# laff
Get a chuckle from a random quip.

Fetches a new utf-8 name, fetches a joke, inserts said name into said joke.

Time spent: ~ 4 hours

Note: I already have a bunch of boilerplate code I've written to launch the  HTTP server and muxer/handler layers (plus the docker files and similar mock servers), so those parts took very little time to stand up.  The bulk of the time was spent on the service implementation, and a good chunk of that was spent trying to work around the rate limiter issues from the name service.

## Introduction and Overview
The solution here implements a web service to retrieve nerdy Chuck Norris jokes tailored to a random name.  It accomplishes this by first fetching a first and last name from a name service, and then passing that name to another "Chuck Norris" joke service, which inserts the name into the joke.  That joke is returned to the user's browser as plain UTF-8 text.

It's actually a little more complicated than that, because for performance, I have implemented two caches, one for names, and one for completed jokes, using Go buffered channels.  There are background worker goroutines that fill the caches to capacity (and then block).  When a user invokes the joke API, the name and jokes are fetched from the channels, but if there is nothing in the channel (the channel read would block), the API fetches these items itself directly.  Note the addition of the background workers should not affect the performance of incoming requests, as these client requests will not have to do HTTP invocations if the items are cached.  In fact, it should be a speedup in that the caches are filled during periods of inactivity.


## Accessing and running the Laff Service program
All external packages are built with go modules, but then vendored, so the complete zip file is runnable.

Here are the steps (a Go toolchain is required - I built with Go 1.13.1):
1. Unzip the zip file anywhere by running `unzip laff.zip`
2. cd to directory "laff"
3. Run `go build .` Note I did not include the binary because I don't know what platform this will be run on.
4. Start the program by running `./laff`.  I actually recommend setting log to "dev" level (Uber zap logging) by running `./laff -log=dev`.  Note the default port is 5000, but the `-port` flag can be used to change that.  There are other configurable options that you can see with `./laff -help`.

In particular, with log level "dev", one can observe how the cache reacts in response to user requests, as well as see it in aciton in the background.

Note in trying to determine whether the rate limiter issue was a platform-specific issue, I also added a docker file and docker compose yaml, so if you want to run under Linux, you can also use `docker-compose up` and `docker-compose down` as an alternative.  I won't cover this approach further here, but it's been tested.

## Invoking the endpoints
There is really only one endpoint in the assignment, which can be invoked by a GET at the server address, for example with `curl http://localhost:5000`.  But to be more formal, this is the list of "standard" endpoints:

* `/v1/status` **GET** a liveness status check
* `/v1/joke`   **GET** same as running the base url as above

## IMPORTANT - Name Service Rate Limiter Issues
The name service at http://uinames.com/api/ imposes *severe* rate limiting to the point where this program can handle only a restricted load.  The code was painstakingly written to be highly robust, concurrent, and scalable, but alas, the rate limiter on the name service kicks in with HTTP 429 and Retry-After response headers after about 10-12 calls in well less than a minute.

Looking at the HTTP repsonse headers, we see: `X-Rate-Limit-Limit: 10.00`, and `X-Rate-Limit-Duration: 1`, so it appears we are actually limited in such a way. 

## Tests
There are a few unit tests in the service package that use a mock name and joke server.  Because I didn't have to worry about the name server rate limiter, I was able to really bang on the algorithm and make sure it could stand up to concurrency.  TestRunLoop is the one that has concurrent requests from 10 threads.  That said, the tests could be more complete, especially negative test cases, given more time to work on this.

## The API

HTTP return codes:
* 200 (OK) for successful requests
* 429 (Too Many Requests) rate limiter issue
* 500 (Internal Server Error) typically won't happen unless there is a system failure

### Architecture and Code Layout
The code has a main package which starts the HTTP server. This package creates a signal handler which is tied to a context cancel function. This allows for clean shutdown. The main code creates a service object. This service is then passed to the api layer, for use with the mux'ed incoming requests.

As mentioned, Uber Zap logging is used. In a real production product, I would have buried it in a logging interface.

Here is a more-specific roadmap of the packages:

### *api* package
Contains the HTTP handlers for the various endpoints. Primary responsibility is to unmarshal incoming requests, convert them to Go objects, and pass them off to the service layer, get the responses back from the service layer, convert any errors (or not) to appropriate HTTP status codes and send them back to the HTTP layer.  Note the external package `tollbooth` rate limiter is applied here as a middleware layer.

### *service* package
The service implements the Laff service and is decoupled from the actual HTTP.  It provides a public API to get a joke, plus it implements internal methods to fetch from the name and joke services, as well as implementing a cache on top of those two services.

## Architecture and Optimizations
There is one service method `Joke()` that handles the user requests.  There is an internal method to fetch the name via HTTP, and another one to fetch a joke, plugging in the retrieved name.  The `Joke()` method can use those directly when needed, but an important feature of the architecture is the name and joke caches.

### Caches
We observe that the names and jokes fetched from the respective services are not in any way time-dependent, so we can fetch names and jokes at any independent times and assemble them into final joke form whenever we choose.  Thus we have two caches, one for names, the other for assembled jokes that are pre-built as the system has time.  The caches are implemented as buffered channels (with a configurable buffer size), and a configurable number of worker goroutines populate the caches.

The name cache is filled independently by one set of goroutines.  Naturally it will block writing the name to the cache until the buffer has room.  The other set of goroutines wait for a name to be available to pull off the name channel.  When a name is read, the Chuck Norris joke endpoint is invoked with that name, and that result is written to the joke buffered channel (as soon as the buffer has space).

When the user invocation reaches the `Joke()` method, the implementation tries to use any available joke in the channel (if `select` says the channel can be read), and if that succeeds, it returns that joke to the caller.  If there is no joke available in the channel, the code first sees if a name is available in the name channel and starts with that.  If not, it invokes the name HTTP API.  In either case it then invokes the joke HTTP API to get the final text to be returned to the user.

### Scalability and Production-Readiness
The caches above are a big part of scalability.  Also I've inserted a configurable rate limiter (the "tollbooth" package) into the middleware layer.  Concurrency works due to each HTTP request being handled in a separate goroutine, along with the inherent thread-safety of channels.  The docker-related files are also part of being production ready, because the service ultimately needs to be deployed somewhere other than my Mac.  I put some deep thought into this architecture and I think it is a good one, but despite that the rate limiter on the name service is too harsh in its limiting to effectively demonstrate the design.

## External packages used

* github.com/didip/tollbooth - rate limiter middleware: MIT License
* github.com/gorilla/mux - HTTP muxer: BSD 3-Clause "New" or "Revised" License
* github.com/pkg/errors - improved error types: BSD 2-Clause "Simplified" License
* go.uber.org/zap (imports as go.uber.org/zap) - efficient logger: Uber license: https://github.com/uber-go/zap/blob/master/LICENSE.txt
