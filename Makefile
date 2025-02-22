# This Makefile is meant to be used by people that do not usually work
# with Go source code. If you know what GOPATH is then you probably
# don't need to bother with make.

.PHONY: grwd android ios grwd-cross swarm evm all test clean
.PHONY: grwd-linux grwd-linux-386 grwd-linux-amd64 grwd-linux-mips64 grwd-linux-mips64le
.PHONY: grwd-linux-arm grwd-linux-arm-5 grwd-linux-arm-6 grwd-linux-arm-7 grwd-linux-arm64
.PHONY: grwd-darwin grwd-darwin-386 grwd-darwin-amd64
.PHONY: grwd-windows grwd-windows-386 grwd-windows-amd64

GOBIN = $(shell pwd)/build/bin
GO ?= latest

grwd:
	build/env.sh go run build/ci.go install ./cmd/grwd
	@echo "Done building."
	@echo "Run \"$(GOBIN)/grwd\" to launch grwd."

swarm:
	build/env.sh go run build/ci.go install ./cmd/swarm
	@echo "Done building."
	@echo "Run \"$(GOBIN)/swarm\" to launch swarm."

all:
	build/env.sh go run build/ci.go install

android:
	build/env.sh go run build/ci.go aar --local
	@echo "Done building."
	@echo "Import \"$(GOBIN)/grwd.aar\" to use the library."

ios:
	build/env.sh go run build/ci.go xcode --local
	@echo "Done building."
	@echo "Import \"$(GOBIN)/Grwd.framework\" to use the library."

test: all
	build/env.sh go run build/ci.go test

lint: ## Run linters.
	build/env.sh go run build/ci.go lint

clean:
	./build/clean_go_build_cache.sh
	rm -fr build/_workspace/pkg/ $(GOBIN)/*

# The devtools target installs tools required for 'go generate'.
# You need to put $GOBIN (or $GOPATH/bin) in your PATH to use 'go generate'.

devtools:
	env GOBIN= go get -u golang.org/x/tools/cmd/stringer
	env GOBIN= go get -u github.com/kevinburke/go-bindata/go-bindata
	env GOBIN= go get -u github.com/fjl/gencodec
	env GOBIN= go get -u github.com/golang/protobuf/protoc-gen-go
	env GOBIN= go install ./cmd/abigen
	@type "npm" 2> /dev/null || echo 'Please install node.js and npm'
	@type "solc" 2> /dev/null || echo 'Please install solc'
	@type "protoc" 2> /dev/null || echo 'Please install protoc'

# Cross Compilation Targets (xgo)

grwd-cross: grwd-linux grwd-darwin grwd-windows grwd-android grwd-ios
	@echo "Full cross compilation done:"
	@ls -ld $(GOBIN)/grwd-*

grwd-linux: grwd-linux-386 grwd-linux-amd64 grwd-linux-arm grwd-linux-mips64 grwd-linux-mips64le
	@echo "Linux cross compilation done:"
	@ls -ld $(GOBIN)/grwd-linux-*

grwd-linux-386:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/386 -v ./cmd/grwd
	@echo "Linux 386 cross compilation done:"
	@ls -ld $(GOBIN)/grwd-linux-* | grep 386

grwd-linux-amd64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/amd64 -v ./cmd/grwd
	@echo "Linux amd64 cross compilation done:"
	@ls -ld $(GOBIN)/grwd-linux-* | grep amd64

grwd-linux-arm: grwd-linux-arm-5 grwd-linux-arm-6 grwd-linux-arm-7 grwd-linux-arm64
	@echo "Linux ARM cross compilation done:"
	@ls -ld $(GOBIN)/grwd-linux-* | grep arm

grwd-linux-arm-5:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/arm-5 -v ./cmd/grwd
	@echo "Linux ARMv5 cross compilation done:"
	@ls -ld $(GOBIN)/grwd-linux-* | grep arm-5

grwd-linux-arm-6:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/arm-6 -v ./cmd/grwd
	@echo "Linux ARMv6 cross compilation done:"
	@ls -ld $(GOBIN)/grwd-linux-* | grep arm-6

grwd-linux-arm-7:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/arm-7 -v ./cmd/grwd
	@echo "Linux ARMv7 cross compilation done:"
	@ls -ld $(GOBIN)/grwd-linux-* | grep arm-7

grwd-linux-arm64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/arm64 -v ./cmd/grwd
	@echo "Linux ARM64 cross compilation done:"
	@ls -ld $(GOBIN)/grwd-linux-* | grep arm64

grwd-linux-mips:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/mips --ldflags '-extldflags "-static"' -v ./cmd/grwd
	@echo "Linux MIPS cross compilation done:"
	@ls -ld $(GOBIN)/grwd-linux-* | grep mips

grwd-linux-mipsle:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/mipsle --ldflags '-extldflags "-static"' -v ./cmd/grwd
	@echo "Linux MIPSle cross compilation done:"
	@ls -ld $(GOBIN)/grwd-linux-* | grep mipsle

grwd-linux-mips64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/mips64 --ldflags '-extldflags "-static"' -v ./cmd/grwd
	@echo "Linux MIPS64 cross compilation done:"
	@ls -ld $(GOBIN)/grwd-linux-* | grep mips64

grwd-linux-mips64le:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/mips64le --ldflags '-extldflags "-static"' -v ./cmd/grwd
	@echo "Linux MIPS64le cross compilation done:"
	@ls -ld $(GOBIN)/grwd-linux-* | grep mips64le

grwd-darwin: grwd-darwin-386 grwd-darwin-amd64
	@echo "Darwin cross compilation done:"
	@ls -ld $(GOBIN)/grwd-darwin-*

grwd-darwin-386:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=darwin/386 -v ./cmd/grwd
	@echo "Darwin 386 cross compilation done:"
	@ls -ld $(GOBIN)/grwd-darwin-* | grep 386

grwd-darwin-amd64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=darwin/amd64 -v ./cmd/grwd
	@echo "Darwin amd64 cross compilation done:"
	@ls -ld $(GOBIN)/grwd-darwin-* | grep amd64

grwd-windows: grwd-windows-386 grwd-windows-amd64
	@echo "Windows cross compilation done:"
	@ls -ld $(GOBIN)/grwd-windows-*

grwd-windows-386:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=windows/386 -v ./cmd/grwd
	@echo "Windows 386 cross compilation done:"
	@ls -ld $(GOBIN)/grwd-windows-* | grep 386

grwd-windows-amd64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=windows/amd64 -v ./cmd/grwd
	@echo "Windows amd64 cross compilation done:"
	@ls -ld $(GOBIN)/grwd-windows-* | grep amd64
