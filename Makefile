BINARY   := yaymlq
PKG      := github.com/reticule-poirot/yaymlq
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X $(PKG)/cmd.version=$(VERSION)

.PHONY: all ci build install test cover lint fmt vet tidy clean run fuzz vulncheck

all: fmt vet lint test build

ci: vet lint vulncheck test build

build:
	go build -ldflags '$(LDFLAGS)' -o bin/$(BINARY) .

install:
	go install -ldflags '$(LDFLAGS)' .

test:
	go test ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -n1

FUZZTIME ?= 20s
fuzz:
	go test ./internal/path    -run '^$$' -fuzz FuzzParse  -fuzztime $(FUZZTIME)
	go test ./internal/query   -run '^$$' -fuzz FuzzRun    -fuzztime $(FUZZTIME)
	go test ./internal/ymledit -run '^$$' -fuzz FuzzSet    -fuzztime $(FUZZTIME)
	go test ./internal/ymledit -run '^$$' -fuzz FuzzAppend -fuzztime $(FUZZTIME)
	go test ./internal/ymledit -run '^$$' -fuzz FuzzDelete -fuzztime $(FUZZTIME)

GOLANGCI_VERSION ?= v2.1.6
lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION) run ./...

vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

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
