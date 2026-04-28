#!/usr/bin/env bash

# Portable defined check
def() {
  [[ -n "${!1+x}" ]]
}

# Source config
export MU_CONFIG_PATH="${MU_PATH}/config.env"
source "$MU_CONFIG_PATH"

# Logic for system type
DEFAULT_SYSTEM='local'
if ! def MU_SYSTEM; then
  export MU_SYSTEM=$DEFAULT_SYSTEM
fi

if [ "$MU_SYSTEM" = "local" ]; then
  export MU_IS_LOCAL='TRUE'
  unset MU_IS_HPC
  export MU_IS_MACOS='TRUE'
elif [ "$MU_SYSTEM" = "hpc" ]; then
  export MU_IS_HPC='TRUE'
  unset MU_IS_LOCAL
  unset MU_IS_MACOS
else
  echo "ERROR: MU_SYSTEM must be 'local' or 'hpc'. Exiting..."
  return 1
fi

# Source all init files recursively
# Using a simple loop that works in bash and zsh
for mod in "${MU_PATH}"/*/*/init.sh; do
  if [ -f "$mod" ]; then
    source "$mod"
  fi
done

source "${MU_PATH}/general.sh"
