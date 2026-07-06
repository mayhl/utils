#!/usr/bin/env sh
# lib/launcher.sh — the `mu` engine launcher.
#
# `mu` is the Go binary at $MU_ROOT/mu (gitignored). Build-on-first-use so a fresh
# checkout just works (go is on PATH via mise / the HPC dev-env). On HPC the
# cross-compiled binary is deployed into place, so no build happens there.
mu() {
  # `mu rebuild` recompiles the engine (dev convenience). Handled here in the shell,
  # NOT as a Go subcommand: a binary that won't compile can't rebuild itself, and
  # rebuild is a dev-only concern that doesn't belong in the deployed binary.
  if [ "$1" = "rebuild" ]; then
    mu_log "INFO" "rebuilding mu…"
    if (cd "${MU_ROOT}" && go build -o mu ./cmd/mu); then
      mu_log "OK" "mu rebuilt (${MU_ROOT}/mu)"
    else
      mu_log "ERROR" "mu: build failed (is go on PATH?)"
      return 1
    fi
    return
  fi
  if [ ! -x "${MU_ROOT}/mu" ]; then
    mu_log "INFO" "building mu (first run)…"
    (cd "${MU_ROOT}" && go build -o mu ./cmd/mu) || {
      mu_log "ERROR" "mu: build failed (is go on PATH?)"
      return 1
    }
  fi
  "${MU_ROOT}/mu" "$@"
}
