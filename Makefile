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
	go test ./internal/query   -run '^$$' -fuzz '^FuzzRun$$' -fuzztime $(FUZZTIME)
	go test ./internal/query   -run '^$$' -fuzz FuzzRunMatchesPathsResolveBack -fuzztime $(FUZZTIME)
	go test ./internal/ymledit -run '^$$' -fuzz '^FuzzSet$$' -fuzztime $(FUZZTIME)
	go test ./internal/ymledit -run '^$$' -fuzz '^FuzzSetMustExistNeverCreates$$' -fuzztime $(FUZZTIME)
	go test ./internal/ymledit -run '^$$' -fuzz FuzzAppend -fuzztime $(FUZZTIME)
	go test ./internal/ymledit -run '^$$' -fuzz FuzzDelete -fuzztime $(FUZZTIME)
	go test ./cmd              -run '^$$' -fuzz FuzzCLI    -fuzztime $(FUZZTIME)
	go test ./internal/ymledit -run '^$$' -fuzz FuzzRename -fuzztime $(FUZZTIME)
	go test ./cmd              -run '^$$' -fuzz FuzzDiff   -fuzztime $(FUZZTIME)
	go test ./cmd              -run '^$$' -fuzz FuzzPathThroughEditScript -fuzztime $(FUZZTIME)
	go test ./internal/editscript -run '^$$' -fuzz FuzzParse -fuzztime $(FUZZTIME)
	go test ./cmd              -run '^$$' -fuzz '^FuzzEditCommentGutters$$' -fuzztime $(FUZZTIME)
	go test ./cmd              -run '^$$' -fuzz '^FuzzGutterWidth$$' -fuzztime $(FUZZTIME)
	go test ./cmd              -run '^$$' -fuzz '^FuzzEditBlockScalars$$' -fuzztime $(FUZZTIME)
	go test ./cmd              -run '^$$' -fuzz '^FuzzEditDocumentMarkers$$' -fuzztime $(FUZZTIME)
	go test ./cmd              -run '^$$' -fuzz '^FuzzEditFlowCollections$$' -fuzztime $(FUZZTIME)
	go test ./cmd              -run '^$$' -fuzz '^FuzzEditTrailingSpace$$' -fuzztime $(FUZZTIME)
	go test ./cmd              -run '^$$' -fuzz '^FuzzEditQuotedScalars$$' -fuzztime $(FUZZTIME)
	go test ./internal/ymledit -run '^$$' -fuzz '^FuzzEditIndexMatchesUncached$$' -fuzztime $(FUZZTIME)

# Must pin a release whose golang.org/x/tools knows the export data version
# the CI toolchain emits: v2.13.2 pins x/tools v0.49.0, which tops out at
# version 4, so Go 1.27.2 broke every lint run with "export data version 5 is
# greater than maximum supported version 4". v2.15.0 pins v0.52.0, which has
# V5. A Go toolchain bump can therefore break lint with no code change.
GOLANGCI_VERSION ?= v2.15.0
lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION) run ./...

GOVULNCHECK_VERSION ?= v1.8.0
vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

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
