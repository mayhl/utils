#!/usr/bin/env bash

# Load the config
source "${MU_PATH}/config.env"

_CACHE_DIR="${HOME}/.cache/mayhl_utils"
_CACHE_FILE="${_CACHE_DIR}/hpc_aliases.sh"

[[ -z "${OSSH}" ]] && export OSSH=$(command -v ssh)

generate_hpc_aliases() {
  mkdir -p "$_CACHE_DIR"

  cat <<'EOC' >"$_CACHE_FILE"
cpHPCWrapper() { 
  mu_log "INFO" "Sync: $1 to $2"; 
  if rsync "${MU_HPC_RSYNC_OPTS}" -e "${OSSH}" "$1" "$2" >> "${HOME}/.cache/mayhl_utils/framework.log" 2>&1; then 
      mu_log "INFO" "Sync success: $1"; 
  else 
      mu_log "ERROR" "Sync failed: $1"; 
      return 1; 
  fi; 
}
cp2HPC() { cpHPCWrapper "$2" "$1:$3"; }
cpHPC() { cpHPCWrapper "$1:$2" "$3"; }
EOC

  # Auto-discover clusters (pattern: MU_.*_HOST$)
  for host_var in $(compgen -v | grep '^MU_.*_HOST$'); do
    HOST_PREFIX=${host_var%_HOST}
    hpcs_var="${HOST_PREFIX}_HPCS"

    eval "hosts_list=\"\$$hpcs_var\""
    eval "base_host=\"\$$host_var\""

    if [[ -n "$hosts_list" ]]; then
      for HOST_HPC in $hosts_list; do
        local HPCL=$(echo "$HOST_HPC" | tr '[:upper:]' '[:lower:]')
        local SSH_TARGET="${MU_HPC_UNAME}@${HPCL}.${base_host}"
        local HPCC="${HPCL^}"

        {
          echo "alias ${HPCL}='${OSSH} ${MU_HPC_SSH_OPTS} \"\$@\" ${SSH_TARGET}'"
          echo "alias cp2${HPCC}='cp2HPC ${SSH_TARGET} \"\$@\"'"
          echo "alias cp${HPCC}='cpHPC ${SSH_TARGET} \"\$@\"'"
        } >>"$_CACHE_FILE"
      done
    fi
  done
}

[[ ! -f "$_CACHE_FILE" ]] && generate_hpc_aliases
source "$_CACHE_FILE"
