BINARY   := yaymlq
PKG      := github.com/reticule-poirot/yaymlq
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X $(PKG)/cmd.version=$(VERSION)

.PHONY: all build install test cover lint fmt vet tidy clean run

all: fmt vet test build

build:
	go build -ldflags '$(LDFLAGS)' -o bin/$(BINARY) .

install:
	go install -ldflags '$(LDFLAGS)' .

test:
	go test ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -n1

lint:
	golangci-lint run

fmt:
	gofmt -w .

vet:
	go vet ./...

tidy:
	go mod tidy

run:
	go run . $(ARGS)

clean:
	rm -rf bin dist coverage.out
