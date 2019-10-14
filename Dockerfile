# Start with a full-fledged golang image, but strip it from the final image.
FROM golang:1.13.1-alpine

WORKDIR /go/src/github.com/gdotgordon/laff

COPY . /go/src/github.com/gdotgordon/laff

RUN go build -v

FROM alpine:latest

WORKDIR /root/

# Make a significantly slimmed-down final result.
COPY --from=0 /go/src/github.com/gdotgordon/laff .

LABEL maintainer="Gary Gordon <gagordon12@gmail.com>"

ENTRYPOINT ["./laff"]
CMD ["--port=8080" "--log=production"]
