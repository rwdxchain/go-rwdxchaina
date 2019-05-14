# Build Grwd in a stock Go builder container
FROM golang:1.10-alpine as builder

RUN apk add --no-cache make gcc musl-dev linux-headers

ADD . /go-rwdxchaina
RUN cd /go-rwdxchaina && make grwd

# Pull Grwd into a second stage deploy alpine container
FROM alpine:latest

RUN apk add --no-cache ca-certificates
COPY --from=builder /go-rwdxchaina/build/bin/grwd /usr/local/bin/

EXPOSE 7464 7465 33760 33760/udp
ENTRYPOINT ["grwd"]
