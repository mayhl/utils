#!/usr/bin/env sh
# shared/connect.sh — ssh / rsync codegen for hopping and copying between
# clusters. Loads everywhere (you hop and copy from an HPC too).
#
# Seam-driven: consumes MU_SSH + mu_auth (set by platform/*.sh); it never
# branches on the platform itself. Portable across bash and zsh — no `compgen`,
# no `${!var}` (uses the explicit MU_CLUSTERS list + mu_indirect instead).
#
# Config contract (config.env):
#   MU_HPC_UNAME               login name on the HPCs
#   MU_CLUSTERS                space-separated cluster (HPC cluster) names
#   MU_CLUSTER_<name>_DOMAIN   FQDN suffix, e.g. alpha.example.mil
#   MU_CLUSTER_<name>_NODES    space-separated machine names on that cluster
# Behavior knobs (defaults.env):
#   MU_HPC_SSH_OPTS            extra ssh options
#   MU_HPC_RSYNC_OPTS          rsync options
#
# Generated aliases, per node <n> (capitalized <N>):
#   <n>        ssh to that machine (auto-authenticates first)
#   cp2<N>     copy local -> that machine   (cp2<N> <local> <remote>)
#   cp<N>      copy that machine -> local   (cp<N> <remote> <local>)

_MU_CACHE_DIR="${HOME}/.cache/mayhl_utils"
_MU_ALIAS_CACHE="${_MU_CACHE_DIR}/hpc_aliases.sh"

mu_connect_generate() {
  mkdir -p "$_MU_CACHE_DIR"

  # rsync copy wrappers. Written literally (single-quoted heredoc) so MU_SSH /
  # opts resolve at call time — the cache stays correct if the seam changes.
  cat << 'EOC' > "$_MU_ALIAS_CACHE"
mu_cp_wrapper() {
  mu_auth
  mu_log "INFO" "Sync: $1 -> $2"
  if rsync ${MU_HPC_RSYNC_OPTS} -e "${MU_SSH}" "$1" "$2" >>"${HOME}/.cache/mayhl_utils/framework.log" 2>&1; then
    mu_log "INFO" "Sync success: $1"
  else
    mu_log "ERROR" "Sync failed: $1"
    return 1
  fi
}
cp2HPC() { mu_cp_wrapper "$2" "$1:$3"; }
cpHPC()  { mu_cp_wrapper "$1:$2" "$3"; }
EOC

  # $(echo ...) splits the space-separated list under both bash and zsh
  # (zsh does not word-split bare $var). Cluster names are lowercase in the
  # list; the config variable names use the ALL-CAPS token (MU_CLUSTER_ALPHA_*).
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
        echo "alias ${nl}='mu_auth && \${MU_SSH} \${MU_HPC_SSH_OPTS} ${target}'"
        echo "alias cp2${nc}='cp2HPC ${target}'"
        echo "alias cp${nc}='cpHPC ${target}'"
      } >> "$_MU_ALIAS_CACHE"
    done
  done
}

# Regenerate the cache and reload it into the current shell.
mu_connect_refresh() {
  rm -f "$_MU_ALIAS_CACHE"
  mu_connect_generate
  # shellcheck source=/dev/null
  . "$_MU_ALIAS_CACHE"
  echo "HPC connection aliases refreshed."
}

# Generate on first use, then load into this shell.
[ -f "$_MU_ALIAS_CACHE" ] || mu_connect_generate
# shellcheck source=/dev/null
. "$_MU_ALIAS_CACHE"
