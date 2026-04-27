#!/bin/bash

# Replace cd command to check if current directory
# is marked as a sshfs parent directory and run connection
# script if so

function cd() {

  SSHFS_DPATH="/Users/rdchlmyl/hpc_sshfs"
  TRG_PATH=$(realpath "$@")

  if [[ $TRG_PATH == ${SSHFS_DPATH}* ]]; then

    if [[ ${TRG_PATH} == "${SSHFS_DPATH}" ]]; then
      builtin cd "$@" || exit

    else

      SIZE=$((${#SSHFS_DPATH} + 1))
      TRG_PATH=${TRG_PATH:$SIZE}

      if ! [[ $TRG_PATH == *"/"* ]]; then

        builtin cd $SSHFS_DPATH || exit

        sh "${MAYHL_UTILS_PATH}/hpc/sshfs/connect.sh" "$TRG_PATH"
        builtin cd "$TRG_PATH" || exit
      else
        builtin cd "$@" || exit
      fi
    fi
  else
    builtin cd "$@" || exit
  fi

}
