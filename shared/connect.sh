#!/usr/bin/env sh
# shared/connect.sh — ssh / rsync codegen for hopping and copying between
# clusters. Loads everywhere (you hop and copy from an HPC too).
#
# Seam-driven: interactive logins use MU_SSH_LOGIN, transfers use MU_SSH, auth
# via mu_auth (all set by platform/*.sh); it never branches on the platform.
# Portable across bash and zsh — no `compgen`, no `${!var}` (uses the explicit
# MU_CLUSTERS list + mu_indirect instead).
#
# Config contract (config.env):
#   MU_HPC_UNAME               login name on the HPCs
#   MU_CLUSTERS                space-separated cluster (HPC cluster) names
#   MU_CLUSTER_<NAME>_DOMAIN   FQDN suffix, e.g. alpha.example.mil
#   MU_CLUSTER_<NAME>_NODES    space-separated machine names on that cluster
# Behavior knobs (defaults.env):
#   MU_HPC_SSH_OPTS            extra ssh options
#   MU_HPC_RSYNC_OPTS         rsync options
#
# Generated aliases, per node <n> (capitalized <N>):
#   <n>        ssh to that machine (auto-authenticates first)
#   cp2<N>     copy local -> that machine   (cp2<N> <local> <remote>)
#   cp<N>      copy that machine -> local   (cp<N> <remote> <local>)

_MU_CACHE_DIR="${HOME}/.cache/mayhl_utils"
_MU_ALIAS_CACHE="${_MU_CACHE_DIR}/hpc_aliases.sh"

# --- static copy helpers (seam-driven; defined here, never cached) -----------
mu_cp_wrapper() {
  mu_auth
  mu_log "INFO" "Sync: $1 -> $2"
  if rsync ${MU_HPC_RSYNC_OPTS} -e "${MU_SSH}" "$1" "$2" >> "${_MU_CACHE_DIR}/framework.log" 2>&1; then
    mu_log "OK" "Sync: $1"
  else
    mu_log "ERROR" "Sync failed: $1"
    return 1
  fi
}
mu_cp_to() { mu_cp_wrapper "$2" "$1:$3"; }   # local -> remote:  mu_cp_to <target> <local> <remote>
mu_cp_from() { mu_cp_wrapper "$1:$2" "$3"; } # remote -> local:  mu_cp_from <target> <remote> <local>

# --- per-cluster alias codegen (cached) --------------------------------------
mu_connect_generate() {
  local c cu domain nodes node nl nc target
  mkdir -p "$_MU_CACHE_DIR"
  : > "$_MU_ALIAS_CACHE"

  [ -n "${MU_HPC_UNAME}" ] || mu_log "WARN" "MU_HPC_UNAME unset; ssh targets will be malformed"

  # $(echo ...) splits the space-separated list under both bash and zsh (zsh
  # does not word-split a bare $var). Names are lowercase in the list; config
  # variable names use the ALL-CAPS token (MU_CLUSTER_ALPHA_*).
  for c in $(echo "$MU_CLUSTERS"); do
    cu=$(printf '%s' "$c" | tr '[:lower:]' '[:upper:]')
    domain=$(mu_indirect "MU_CLUSTER_${cu}_DOMAIN")
    nodes=$(mu_indirect "MU_CLUSTER_${cu}_NODES")
    [ -n "$domain" ] || {
      mu_log "WARN" "Cluster '${c}' has no _DOMAIN; skipping"
      continue
    }

    for node in $(echo "$nodes"); do
      nl=$(printf '%s' "$node" | tr '[:upper:]' '[:lower:]')
      nc=$(mu_capitalize "$nl")
      target="${MU_HPC_UNAME}@${nl}.${domain}"
      {
        echo "alias ${nl}='mu_auth && \${MU_SSH_LOGIN} \${MU_HPC_SSH_OPTS} ${target}'"
        echo "alias cp2${nc}='mu_cp_to ${target}'"
        echo "alias cp${nc}='mu_cp_from ${target}'"
      } >> "$_MU_ALIAS_CACHE"
    done
  done
}

# Regenerate the cache and reload it into the current shell.
mu_connect_refresh() {
  mu_connect_generate
  # shellcheck source=/dev/null
  . "$_MU_ALIAS_CACHE"
  echo "HPC connection aliases refreshed."
}

# Cache is stale if missing, or older than the tool template or machine config.
_mu_cache_stale() {
  [ -f "$_MU_ALIAS_CACHE" ] || return 0
  [ "${MU_ROOT}/shared/connect.sh" -nt "$_MU_ALIAS_CACHE" ] && return 0
  [ -f "${MU_ROOT}/config.env" ] && [ "${MU_ROOT}/config.env" -nt "$_MU_ALIAS_CACHE" ] && return 0
  return 1
}

_mu_cache_stale && mu_connect_generate
# shellcheck source=/dev/null
. "$_MU_ALIAS_CACHE"
