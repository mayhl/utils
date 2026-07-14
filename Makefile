# mayhl_utils — Go engine tasks.
#
# Formatters/linters are the source of truth via CLI (gofumpt / golangci-lint);
# the git pre-commit hook and the editor (conform/nvim-lint) call the same tools,
# so all three agree. See global CLAUDE.md "formatting workflow".

GO      ?= go
BIN     := mu
PKG     := ./cmd/mu
# gofumpt and golangci-lint are NOT on PATH — they live in mise (config.fmt.toml, opt-in
# via MU_MODULES∋fmt), so every call goes through `mise exec`. Naming them bare here made
# fmt/lint silently unrunnable.
MISE      ?= mise
GOFUMPT   := $(MISE) exec gofumpt@latest -- gofumpt
GOLANGCI  := $(MISE) exec golangci-lint@latest -- golangci-lint
# Version stamp: `git describe` gives tag + commits-since + short SHA + -dirty, or just
# the SHA until the first tag (--always). Injected into the binary via -X; mu reports it
# as `mu --version`. Signed tags (`git tag -s vX.Y.Z`) turn the SHA into real semver.
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null)
LDFLAGS := -s -w -X github.com/mayhl/mayhl_utils/internal/cli.version=$(VERSION)

.PHONY: check fmt fmt-check lint lint-advisory vet build build-linux test tidy clean

# check is the pre-commit gate in one command — run it before EVERY commit. Build first (a
# broken build makes the rest noise), formatting as a CHECK not a rewrite (a gate that edits
# your tree mid-commit is a surprise; `make fmt` is the rewrite). Lint is ADVISORY: the repo
# carries a known backlog (errcheck on Fprint*, one ineffassign, one staticcheck QF), and a
# gate that is red on a clean tree teaches you to ignore it. It prints; it doesn't block.
# Flip it blocking (swap in `lint`) once the backlog is cleared.
check: build vet fmt-check test lint-advisory ## The pre-commit gate: build + vet + fmt + test, lint advisory

fmt: ## Format all Go sources (gofumpt)
	$(GOFUMPT) -w .

fmt-check: ## Fail (listing the files) if anything is unformatted
	@bad=$$($(GOFUMPT) -l .); \
	if [ -n "$$bad" ]; then echo "unformatted (run 'make fmt'):"; echo "$$bad"; exit 1; fi

lint: ## Lint (golangci-lint: staticcheck + govet + errcheck + …)
	$(GOLANGCI) run

lint-advisory: ## Lint, reporting only — the gate's non-blocking pass over the known backlog
	@echo "lint (advisory — known backlog, not a gate failure):"
	-@$(GOLANGCI) run

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
