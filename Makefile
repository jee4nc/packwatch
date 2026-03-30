BINARY    := packwatch
MODULE    := github.com/jee4nc/packwatch
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT    := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE:= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GOFLAGS   := -trimpath -buildvcs=false
LDFLAGS   := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.buildDate=$(BUILD_DATE)

.PHONY: build install clean run lint fmt vet test release

## build: compile for current OS/arch
build:
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BINARY) .

## run: build and run
run: build
	./bin/$(BINARY)

## install: install to $GOPATH/bin
install:
	go install $(GOFLAGS) -ldflags '$(LDFLAGS)' .

## fmt: format code
fmt:
	gofmt -s -w .

## vet: run go vet
vet:
	go vet ./...

## lint: fmt + vet
lint: fmt vet

## test: run tests
test:
	go test ./... -v

## clean: remove build artifacts
clean:
	rm -rf bin/

## release: cross-compile for common targets
release: clean
	@mkdir -p bin
	GOOS=darwin  GOARCH=arm64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BINARY)-darwin-arm64      .
	GOOS=darwin  GOARCH=amd64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BINARY)-darwin-amd64      .
	GOOS=linux   GOARCH=amd64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BINARY)-linux-amd64       .
	GOOS=linux   GOARCH=arm64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BINARY)-linux-arm64       .
	GOOS=windows GOARCH=amd64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BINARY)-windows-amd64.exe .
	GOOS=windows GOARCH=arm64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BINARY)-windows-arm64.exe .
	@echo "Built $(VERSION) binaries in bin/"

## help: show this help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | column -t -s ':'
