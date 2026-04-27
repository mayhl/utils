#!/usr/bin/env bash

# Load the config
source "${MAYHL_UTILS_PATH}/config.env"

# Use a common cache directory
_CACHE_DIR="${HOME}/.cache/mayhl_utils"
_CACHE_FILE="${_CACHE_DIR}/hpc_aliases.sh"

# Ensure OSSH is set
if [[ -z "${OSSH}" ]]; then
  if [[ -f "/usr/local/ossh/bin/ssh" ]]; then
    export OSSH="/usr/local/ossh/bin/ssh"
  else
    export OSSH=$(command -v ssh)
  fi
fi

# Function to generate the alias file
generate_hpc_aliases() {
  mkdir -p "$_CACHE_DIR"

  cat <<'EOC' >"$_CACHE_FILE"
cpHPCWrapper() { rsync "${HPC_RSYNC_OPTS}" -e "${OSSH}" "$1" "$2"; }
cp2HPC() { cpHPCWrapper "$2" "$1:$3"; }
cpHPC() { cpHPCWrapper "$1:$2" "$3"; }
EOC

  for HPC_HOST_VAR in $HPC_HOSTS; do
    HOST_NAME=$(echo "$HPC_HOST_VAR" | tr '[:lower:]' '[:upper:]')

    hpcs_var="${HOST_NAME}_HPCS"
    host_var="${HOST_NAME}_HOST"

    # Indirect expansion: works in bash and zsh when accessed via eval or local indirect
    # Let's use eval to be safe across both
    eval "local hosts_list=\"\$$hpcs_var\""
    eval "local base_host=\"\$$host_var\""

    for HOST_HPC in $hosts_list; do
      local HPCL=$(echo "$HOST_HPC" | tr '[:upper:]' '[:lower:]')
      local SSH_TARGET="${HPC_UNAME}@${HPCL}.${base_host}"
      local HPCC="${HPCL^}"

      {
        echo "alias ${HPCL}='${OSSH} ${HPC_SSH_OPTS} \"\$@\" ${SSH_TARGET}'"
        echo "alias cp2${HPCC}='cp2HPC ${SSH_TARGET} \"\$@\"'"
        echo "alias cp${HPCC}='cpHPC ${SSH_TARGET} \"\$@\"'"
      } >>"$_CACHE_FILE"
    done
  done
}

# Gatekeeper
if [[ ! -f "$_CACHE_FILE" ]]; then
  generate_hpc_aliases
fi

source "$_CACHE_FILE"
