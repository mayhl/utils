#!/usr/bin/env sh
# lib/compat.sh — OS/shell portability shims.
#
# Pure functions, no side effects. Sourced first by init.sh so every later
# module can stay written once and run under both bash and zsh, macOS (BSD)
# and Linux (GNU). Nothing here branches on MU_SYSTEM — these axes are
# DETECTED, not derived from the mode toggle.

# --- detection --------------------------------------------------------------
mu_is_macos() { [ "$(uname -s)" = "Darwin" ]; }
mu_is_zsh() { [ -n "$ZSH_VERSION" ]; }
mu_have() { command -v "$1" > /dev/null 2>&1; }

# --- indirect expansion (bash ${!v} / zsh ${(P)v}) --------------------------
# eval is the one form both shells parse; the alternatives are mutually
# exclusive parse-time syntax. $1 must be a bare variable name.
mu_indirect() { eval "printf '%s' \"\${$1}\""; }

# --- capitalize first letter (bash ${x^} / zsh ${(C)x}) ---------------------
mu_capitalize() {
  local rest head
  rest=${1#?}       # everything after the first char
  head=${1%"$rest"} # the first char
  printf '%s%s' "$(printf '%s' "$head" | tr '[:lower:]' '[:upper:]')" "$rest"
}

# --- coreutils shims (BSD/macOS vs GNU/Linux) -------------------------------
# GNU cut (macOS ships BSD cut as `cut`; GNU as `gcut` via coreutils)
mu_cut() {
  if mu_is_macos && mu_have gcut; then gcut "$@"; else cut "$@"; fi
}

# in-place sed (BSD requires an explicit empty backup suffix)
mu_sed_i() {
  if mu_is_macos; then sed -i '' "$@"; else sed -i "$@"; fi
}

# realpath (may be absent on stock macOS)
mu_realpath() {
  if mu_have realpath; then
    realpath "$@"
  elif mu_have grealpath; then
    grealpath "$@"
  else
    # POSIX fallback for a single path argument
    (cd "$(dirname -- "$1")" 2> /dev/null && printf '%s/%s\n' "$(pwd -P)" "$(basename -- "$1")")
  fi
}

# directory size in bytes (GNU `du -sb`; BSD du has no -b, reports KiB blocks)
mu_du_bytes() {
  local kb
  if du -sb "$1" > /dev/null 2>&1; then
    du -sb "$1" | cut -f1
  else
    kb=$(du -sk "$1" | cut -f1)
    echo $((kb * 1024))
  fi
}
