# mayhl_utils — Go engine tasks.
#
# Formatters/linters are the source of truth via CLI (gofumpt / golangci-lint);
# the git pre-commit hook and the editor (conform/nvim-lint) call the same tools,
# so all three agree. See global CLAUDE.md "formatting workflow".

GO      ?= go
BIN     := mu
PKG     := ./cmd/mu
LDFLAGS := -s -w

.PHONY: fmt lint vet build build-linux test tidy clean

fmt: ## Format all Go sources (gofumpt)
	gofumpt -w .

lint: ## Lint (golangci-lint: staticcheck + govet + errcheck + …)
	golangci-lint run

vet:
	$(GO) vet ./...

build: ## Native build for this machine
	$(GO) build -o $(BIN) $(PKG)

build-linux: ## Static linux/amd64 binary for HPC deploy (no libc dependency)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -ldflags '$(LDFLAGS)' -o $(BIN)-linux-amd64 $(PKG)

test: ## Run the whole test suite (go test ./...)
	$(GO) test ./...

tidy:
	$(GO) mod tidy

clean:
	rm -f $(BIN) $(BIN)-linux-amd64
