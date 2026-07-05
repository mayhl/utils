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
. "${MU_ROOT}/lib/launcher.sh"
mkdir -p "${HOME}/.cache/mayhl_utils"

# ---- config: tracked defaults; the cluster/user config lives in config.toml,
# read by the mu engine and exported to the shell by `mu shell-init` below -----
[ -f "${MU_ROOT}/defaults.env" ] && . "${MU_ROOT}/defaults.env"
[ -f "${MU_ROOT}/config.toml" ] || [ -n "${MU_CONFIG_FILE}" ] ||
  mu_log "WARN" "No config.toml found; copy config.toml.example to config.toml and fill it in."

# ---- mode: detected from $BC_HOST, MU_SYSTEM is an optional override --------
# $BC_HOST is the HPC system name (set on HPC login + compute nodes, absent on
# a workstation). If MU_SYSTEM was not set explicitly, derive it; an explicit
# value always wins.
if [ -z "${MU_SYSTEM}" ]; then
  if [ -n "${BC_HOST}" ]; then MU_SYSTEM=hpc; else MU_SYSTEM=local; fi
fi
case "$MU_SYSTEM" in
  local | hpc) ;;
  *)
    printf 'ERROR: MU_SYSTEM must be "local" or "hpc" (got "%s")\n' "$MU_SYSTEM" >&2
    return 1 2> /dev/null || exit 1
    ;;
esac
export MU_SYSTEM

# OS compat is DETECTED, never derived from the mode toggle.
if mu_is_macos; then export MU_IS_MACOS=TRUE; else unset MU_IS_MACOS; fi

# ---- platform seam (sets MU_SSH + mu_auth) ---------------------------------
. "${MU_ROOT}/platform/${MU_SYSTEM}.sh"

# ---- connectivity: per-node dispatchers (mike / mike push|pull / mike <cmd>)
# plus the config as MU_* exports, generated from config.toml by the Go engine.
# Replaces the old connect.sh alias codegen. -----------------------------------
eval "$(mu shell-init)"

# ---- shared tooling --------------------------------------------------------
. "${MU_ROOT}/shared/tar.sh"
. "${MU_ROOT}/shared/aliases.sh"
. "${MU_ROOT}/shared/status.sh"
. "${MU_ROOT}/shared/utils.sh"

# ---- machine-specific customizations (gitignored, optional, sourced last) --
[ -f "${MU_ROOT}/custom.sh" ] && . "${MU_ROOT}/custom.sh"
