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

# FUZZ_JOBS bounds how many of the targets below make runs at once (-j).
# They're 20 independent go test invocations across 5 packages with no
# shared state, so running them concurrently is a real lever on make
# fuzz's wall time, not just a rewrite — but go test -fuzz defaults its own
# worker count to GOMAXPROCS, so running FUZZ_JOBS of them at once would
# oversubscribe a FUZZ_JOBS-core machine by a factor of FUZZ_JOBS. Each
# target pins GOMAXPROCS=1 for exactly that reason: the outer -j is what
# spreads work across cores, not the inner fuzzer.
FUZZ_JOBS ?= $(shell nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)

FUZZ_TARGETS := \
	fuzz-path-parse \
	fuzz-query-run \
	fuzz-query-runmatches-paths \
	fuzz-ymledit-set \
	fuzz-ymledit-setmustexist \
	fuzz-ymledit-append \
	fuzz-ymledit-delete \
	fuzz-cmd-cli \
	fuzz-ymledit-rename \
	fuzz-cmd-diff \
	fuzz-cmd-paththroughedit \
	fuzz-editscript-parse \
	fuzz-cmd-edit \
	fuzz-cmd-editblockscalars \
	fuzz-cmd-editdocumentmarkers \
	fuzz-cmd-editflowcollections \
	fuzz-cmd-editquotedscalars \
	fuzz-cmd-editboundaryblanks \
	fuzz-cmd-splice \
	fuzz-ymledit-editindexuncached

.PHONY: $(FUZZ_TARGETS)
fuzz:
	$(MAKE) -j$(FUZZ_JOBS) $(FUZZ_TARGETS)

fuzz-path-parse:
	GOMAXPROCS=1 go test ./internal/path    -run '^$$' -fuzz FuzzParse  -fuzztime $(FUZZTIME)
fuzz-query-run:
	GOMAXPROCS=1 go test ./internal/query   -run '^$$' -fuzz '^FuzzRun$$' -fuzztime $(FUZZTIME)
fuzz-query-runmatches-paths:
	GOMAXPROCS=1 go test ./internal/query   -run '^$$' -fuzz FuzzRunMatchesPathsResolveBack -fuzztime $(FUZZTIME)
fuzz-ymledit-set:
	GOMAXPROCS=1 go test ./internal/ymledit -run '^$$' -fuzz '^FuzzSet$$' -fuzztime $(FUZZTIME)
fuzz-ymledit-setmustexist:
	GOMAXPROCS=1 go test ./internal/ymledit -run '^$$' -fuzz '^FuzzSetMustExistNeverCreates$$' -fuzztime $(FUZZTIME)
fuzz-ymledit-append:
	GOMAXPROCS=1 go test ./internal/ymledit -run '^$$' -fuzz FuzzAppend -fuzztime $(FUZZTIME)
fuzz-ymledit-delete:
	GOMAXPROCS=1 go test ./internal/ymledit -run '^$$' -fuzz FuzzDelete -fuzztime $(FUZZTIME)
fuzz-cmd-cli:
	GOMAXPROCS=1 go test ./cmd              -run '^$$' -fuzz FuzzCLI    -fuzztime $(FUZZTIME)
fuzz-ymledit-rename:
	GOMAXPROCS=1 go test ./internal/ymledit -run '^$$' -fuzz FuzzRename -fuzztime $(FUZZTIME)
fuzz-cmd-diff:
	GOMAXPROCS=1 go test ./cmd              -run '^$$' -fuzz FuzzDiff   -fuzztime $(FUZZTIME)
fuzz-cmd-paththroughedit:
	GOMAXPROCS=1 go test ./cmd              -run '^$$' -fuzz FuzzPathThroughEditScript -fuzztime $(FUZZTIME)
fuzz-editscript-parse:
	GOMAXPROCS=1 go test ./internal/editscript -run '^$$' -fuzz FuzzParse -fuzztime $(FUZZTIME)
fuzz-cmd-edit:
	GOMAXPROCS=1 go test ./cmd              -run '^$$' -fuzz '^FuzzEdit$$' -fuzztime $(FUZZTIME)
fuzz-cmd-editblockscalars:
	GOMAXPROCS=1 go test ./cmd              -run '^$$' -fuzz '^FuzzEditBlockScalars$$' -fuzztime $(FUZZTIME)
fuzz-cmd-editdocumentmarkers:
	GOMAXPROCS=1 go test ./cmd              -run '^$$' -fuzz '^FuzzEditDocumentMarkers$$' -fuzztime $(FUZZTIME)
fuzz-cmd-editflowcollections:
	GOMAXPROCS=1 go test ./cmd              -run '^$$' -fuzz '^FuzzEditFlowCollections$$' -fuzztime $(FUZZTIME)
fuzz-cmd-editquotedscalars:
	GOMAXPROCS=1 go test ./cmd              -run '^$$' -fuzz '^FuzzEditQuotedScalars$$' -fuzztime $(FUZZTIME)
fuzz-cmd-editboundaryblanks:
	GOMAXPROCS=1 go test ./cmd              -run '^$$' -fuzz '^FuzzEditBoundaryBlanks$$' -fuzztime $(FUZZTIME)
fuzz-cmd-splice:
	GOMAXPROCS=1 go test ./cmd              -run '^$$' -fuzz '^FuzzSplice$$' -fuzztime $(FUZZTIME)
fuzz-ymledit-editindexuncached:
	GOMAXPROCS=1 go test ./internal/ymledit -run '^$$' -fuzz '^FuzzEditIndexMatchesUncached$$' -fuzztime $(FUZZTIME)

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
