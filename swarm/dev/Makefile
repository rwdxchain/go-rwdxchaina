.PHONY: build cluster test

default: build

build:
	go build -o bin/swarm    github.com/rwdxchain/go-rwdxchaina/cmd/swarm
	go build -o bin/grwd     github.com/rwdxchain/go-rwdxchaina/cmd/grwd
	go build -o bin/bootnode github.com/rwdxchain/go-rwdxchaina/cmd/bootnode

cluster: build
	scripts/boot-cluster.sh

test:
	go test -v github.com/rwdxchain/go-rwdxchaina/swarm/...
