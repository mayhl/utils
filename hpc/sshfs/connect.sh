#!/usr/bin/env bash

MPATH=${1%/}
MAPATH=$PWD/$MPATH
EPATH=".${MPATH}.env"

if [ ! -d "$MPATH" ]; then
  echo "ERROR: Directory '$MPATH' does not exist!"
  exit 1
fi

if [ ! -f "${EPATH}" ]; then
  echo "ERROR: Mount script '${EPATH}' does not exist!"
  exit 1
fi

source $EPATH
source "${MAYHL_UTILS_PATH}/hpc/sshfs/spinner.sh"

# Check if path is already mounted with macfuse/sshfs
MPATHS=$(mount | grep fuse-t | cut -d ' ' -f 3)
IS_MOUNTED=F
for TMP_MPATH in $MPATHS; do
  if [[ $MAPATH == $TMP_MPATH ]]; then
    IS_MOUNTED=T
    break
  fi
done

# Checking if path need to be mounted
if [[ $IS_MOUNTED == "T" ]]; then

  # Check if mount connection is working
  ls $MAPATH 1>/dev/null 2>/dev/null
  if [ $? -eq 0 ]; then
    # Mount connection working
    MOUNT=F
  else
    # Mount connect not working
    echo "WARNING: Reestablishing mount"
    umount -f $MAPATH
    MOUNT=T
  fi
else
  # Not mounted already so mounting
  MOUNT=T
fi

if [[ $MOUNT == 'T' ]]; then

  echo -n "Connecting "

  HPC_HOST="mayhl@node1.alpha.example.mil"
  HPC_DPATH="/p/home/mayhl/repos/mayhl-FUNWAVE-TVD"
  #  sshfs -o allow_other $HPATH $MAPATH &

  ARGS=(-o defer_permissions)
  sshfs "${ARGS[@]}" "${HPC_HOST}:${HPC_DPATH}" "${MAPATH}"

  spinner $! #"${OPTS[@]}"

  echo ""
  if [ $? -eq 0 ]; then
    echo "Mount connection established."
  else
    echo "ERROR: Failed to establish mount connection!"
  fi

fi
