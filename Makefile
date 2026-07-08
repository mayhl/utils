# mayhl_utils — Go engine tasks.
#
# Formatters/linters are the source of truth via CLI (gofumpt / golangci-lint);
# the git pre-commit hook and the editor (conform/nvim-lint) call the same tools,
# so all three agree. See global CLAUDE.md "formatting workflow".

GO      ?= go
BIN     := mu
PKG     := ./cmd/mu
# Version stamp: `git describe` gives tag + commits-since + short SHA + -dirty, or just
# the SHA until the first tag (--always). Injected into the binary via -X; mu reports it
# as `mu --version`. Signed tags (`git tag -s vX.Y.Z`) turn the SHA into real semver.
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null)
LDFLAGS := -s -w -X github.com/mayhl/mayhl_utils/internal/cli.version=$(VERSION)

.PHONY: fmt lint vet build build-linux test tidy clean

fmt: ## Format all Go sources (gofumpt)
	gofumpt -w .

lint: ## Lint (golangci-lint: staticcheck + govet + errcheck + …)
	golangci-lint run

vet:
	$(GO) vet ./...

build: ## Native build for this machine (version-stamped)
	$(GO) build -ldflags '$(LDFLAGS)' -o $(BIN) $(PKG)

build-linux: ## Static linux/amd64 binary for HPC deploy (no libc dependency)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -ldflags '$(LDFLAGS)' -o $(BIN)-linux-amd64 $(PKG)

test: ## Run the whole test suite (go test ./...)
	$(GO) test ./...

tidy:
	$(GO) mod tidy

clean:
	rm -f $(BIN) $(BIN)-linux-amd64
