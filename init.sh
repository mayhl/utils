#!/usr/bin/env sh
# mayhl_utils entry point.
#
# Load order:  compat -> log -> config (defaults, then machine) ->
#              platform seam (MU_SYSTEM) -> shared tooling.
#
# This file knows only env-var contracts; it must not reference .config paths.
# Caller (.config) sets MU_ROOT and MU_SYSTEM, then sources this file.

: "${MU_ROOT:?MU_ROOT must point at the mayhl_utils checkout}"

# ---- portability shims (no side effects) -----------------------------------
. "${MU_ROOT}/lib/compat.sh"
. "${MU_ROOT}/lib/log.sh"
mkdir -p "${HOME}/.cache/mayhl_utils"

# ---- config: tracked defaults, then machine overrides ----------------------
[ -f "${MU_ROOT}/defaults.env" ] && . "${MU_ROOT}/defaults.env"
if [ -f "${MU_ROOT}/config.env" ]; then
  . "${MU_ROOT}/config.env"
else
  mu_log "WARN" "No config.env found; copy config.env.example to config.env and fill it in."
fi

# ---- mode toggle (binary: local | hpc) -------------------------------------
: "${MU_SYSTEM:=local}"
case "$MU_SYSTEM" in
  local | hpc) ;;
  *)
    printf 'ERROR: MU_SYSTEM must be "local" or "hpc" (got "%s")\n' "$MU_SYSTEM" >&2
    return 1 2> /dev/null || exit 1
    ;;
esac

# OS compat is DETECTED, never derived from the mode toggle.
if mu_is_macos; then export MU_IS_MACOS=TRUE; else unset MU_IS_MACOS; fi

# ---- platform seam (sets MU_SSH + mu_auth) ---------------------------------
. "${MU_ROOT}/platform/${MU_SYSTEM}.sh"

# ---- shared tooling --------------------------------------------------------
. "${MU_ROOT}/shared/connect.sh"
. "${MU_ROOT}/shared/tar.sh"
. "${MU_ROOT}/shared/git.sh"
. "${MU_ROOT}/shared/aliases.sh"
. "${MU_ROOT}/shared/status.sh"
. "${MU_ROOT}/shared/utils.sh"

# ---- machine-specific customizations (gitignored, optional, sourced last) --
[ -f "${MU_ROOT}/custom.sh" ] && . "${MU_ROOT}/custom.sh"
