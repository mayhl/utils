#!/usr/bin/env bash
# shellcheck disable=SC1090

# TODO:
#   [] Add script validation?
#   [] Add optional config path, e.g., ~/.configs
#

def () {
  [[ ! -z "${(tP)1}" ]]
}

MAYHL_UTILS_CONFIG_PATH="${0:a:h}"/config.env

if [ ! -n "${MAYHL_UTILS_PATH}" ]; then
  echo "ERROR: MAYHL_UTILS_PATH not set. Exiting .zshrc script..."
  return 1
fi

# Checking if system type has been set
DEFAULT_SYSTEM='local'
if ! def MAYHL_UTILS_SYSTEM; then
  #if [ -z "${MAYHL_UTILS_SYSTEM}" ]; then
  echo "WARNING: MAYHL_UTILS_SYSTEM not set. Defaulting to ${DEFAULT_SYSTEM}..."
  MAYHL_UTILS_SYSTEM=$DEFAULT_SYSTEM
fi

if [ "${MAYHL_UTILS_SYSTEM}" = "local" ]; then
  export MAYHL_UTILS_IS_LOCAL='TRUE'
  unset MAYHL_UTILS_IS_HPC

  # TODO: Convert to MacOS check
  # shellcheck disable=2050
  if [ 'a' = 'a' ]; then
    export MAYHL_UTILS_IS_MACOS='TRUE'
  else
    unset MAYHL_UTILS_IS_MACOS
  fi
elif [ "${MAYHL_UTILS_SYSTEM}" = "hpc" ]; then
  export MAYHL_UTILS_IS_HPC='TRUE'
  unset MAYHL_UTILS_IS_LOCAL

  # No MacOS HPCs
  unset MAYHL_UTILS_IS_MACOS
else
  echo "ERROR: MAYHL_UTILS_SYSTEM must either be 'local' or 'hpc'. Exiting .zshrc script..."
  return 1
fi
unset DEFAULT_SYSTEM

#echo "CONFIG PATH:" $MAYHL_UTILS_CONFIG_PATH
source "$MAYHL_UTILS_CONFIG_PATH"

source "${0:a:h}"/general.sh

source "${0:a:h}"/hpc/init.sh
source "${0:a:h}"/tar/init.sh
#source ${HPC_CMDS_PATH}/pbs/main.sh
#source ${HPC_CMDS_PATH}/rsync_cmds.sh
#source ${HPC_CMDS_PATH}/swap_work_home/main.sh
#source ${HPC_CMDS_PATH}/
#source ${HPC_CMDS_PATH}/archive/main.sh
