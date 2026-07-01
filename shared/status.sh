#!/usr/bin/env sh
# shared/status.sh — environment status / context reporting.
# Driven by the explicit MU_CLUSTERS list + mu_indirect (no compgen/${!}).

# Compact one-field-per-line summary.
mu_status() {
  echo "--- mayhl_utils status ---"
  echo "System:   ${MU_SYSTEM:-unset}"
  echo "Root:     ${MU_ROOT:-unset}"
  echo "Git:      $(git -C "${MU_ROOT}" rev-parse --short HEAD 2> /dev/null || echo 'n/a')"
  echo "SSH:      ${MU_SSH:-unset}"
  echo "User:     ${MU_HPC_UNAME:-unset}"
  echo "Clusters: ${MU_CLUSTERS:-none}"
}

# Detailed context: per-cluster domain + nodes.
mu_ctx() {
  printf '\033[1;36m--- mayhl_utils context ---\033[0m\n'
  printf 'System: %s   User: %s   Root: %s\n' \
    "${MU_SYSTEM:-unset}" "${MU_HPC_UNAME:-unset}" "${MU_ROOT:-unset}"
  printf '\033[1;36m--- clusters ---\033[0m\n'
  if [ -z "${MU_CLUSTERS}" ]; then
    echo "  (none configured)"
    return
  fi
  local c cu domain nodes
  for c in $(echo "$MU_CLUSTERS"); do
    cu=$(printf '%s' "$c" | tr '[:lower:]' '[:upper:]')
    domain=$(mu_indirect "MU_CLUSTER_${cu}_DOMAIN")
    nodes=$(mu_indirect "MU_CLUSTER_${cu}_NODES")
    printf '  %-8s %-22s [%s]\n' "$c" "${domain:-?}" "$nodes"
  done
}
