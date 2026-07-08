#!/usr/bin/env sh
# lib/launcher.sh — the `mu` engine launcher.
#
# `mu` is the Go binary at $MU_ROOT/mu (gitignored). Build-on-first-use so a fresh
# checkout just works (go is on PATH via mise / the HPC dev-env). On HPC the
# cross-compiled binary is deployed into place, so no build happens there.

# _mu_build compiles the engine with a version stamp — `git describe` (tag + commits +
# short SHA + -dirty, or the SHA until the first tag) becomes `mu --version`. Matches the
# Makefile and onboard cross-build. Runs in MU_ROOT so describe reads this repo.
_mu_build() {
  (cd "${MU_ROOT}" && go build \
    -ldflags "-s -w -X github.com/mayhl/mayhl_utils/internal/cli.version=$(git describe --tags --always --dirty 2> /dev/null)" \
    -o mu ./cmd/mu)
}

mu() {
  # `mu rebuild` recompiles the engine (dev convenience). Handled here in the shell,
  # NOT as a Go subcommand: a binary that won't compile can't rebuild itself, and
  # rebuild is a dev-only concern that doesn't belong in the deployed binary.
  if [ "$1" = "rebuild" ]; then
    mu_log "INFO" "rebuilding mu…"
    if _mu_build; then
      mu_log "OK" "mu rebuilt (${MU_ROOT}/mu)"
    else
      mu_log "ERROR" "mu: build failed (is go on PATH?)"
      return 1
    fi
    return
  fi
  if [ ! -x "${MU_ROOT}/mu" ]; then
    mu_log "INFO" "building mu (first run)…"
    _mu_build || {
      mu_log "ERROR" "mu: build failed (is go on PATH?)"
      return 1
    }
  fi
  "${MU_ROOT}/mu" "$@"
}
