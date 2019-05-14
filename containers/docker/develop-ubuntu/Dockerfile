FROM ubuntu:xenial

ENV PATH=/usr/lib/go-1.9/bin:$PATH

RUN \
  apt-get update && apt-get upgrade -q -y && \
  apt-get install -y --no-install-recommends golang-1.9 git make gcc libc-dev ca-certificates && \
  git clone --depth 1 https://github.com/rwdxchain/go-rwdxchaina && \
  (cd go-rwdxchaina && make grwd) && \
  cp go-rwdxchaina/build/bin/grwd /grwd && \
  apt-get remove -y golang-1.9 git make gcc libc-dev && apt autoremove -y && apt-get clean && \
  rm -rf /go-rwdxchaina

EXPOSE 7464
EXPOSE 33760

ENTRYPOINT ["/grwd"]
