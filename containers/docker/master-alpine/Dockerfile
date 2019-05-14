FROM alpine:3.7

RUN \
  apk add --update go git make gcc musl-dev linux-headers ca-certificates && \
  git clone --depth 1 --branch release/1.8 https://github.com/rwdxchain/go-rwdxchaina && \
  (cd go-rwdxchaina && make grwd) && \
  cp go-rwdxchaina/build/bin/grwd /grwd && \
  apk del go git make gcc musl-dev linux-headers && \
  rm -rf /go-rwdxchaina && rm -rf /var/cache/apk/*

EXPOSE 7464
EXPOSE 33760

ENTRYPOINT ["/grwd"]
