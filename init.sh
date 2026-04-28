#!/usr/bin/env bash

# Portable defined check (Works in Bash 4.2+ and Zsh)
def() {
  [[ -v "$1" ]]
}

# Source config
source "${MU_ROOT}/config.env"

# Logic for system type
DEFAULT_SYSTEM='local'
if ! def MU_SYSTEM; then
  echo "WARNING: MU_SYSTEM not set. Defaulting to ${DEFAULT_SYSTEM}..."
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
for mod in "${MU_ROOT}"/*/*/init.sh; do
  if [ -f "$mod" ]; then
    source "$mod"
  fi
done

source "${MU_ROOT}/general.sh"
source "${MU_ROOT}/hpc/init.sh"
