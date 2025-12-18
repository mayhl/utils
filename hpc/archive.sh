#!/usr/bin/env bash

# TODO: Remove
ARC_PBS_PATH="${0:a:h}/archive.pbs"

function qarc() {

  OLD_ROOT=${WORKDIR}/projects
  NEW_ROOT=projects/workdir

  local DIR_RPATH=$1

  if [ ! -d "$DIR_RPATH" ]; then
    echo "$DIR_RPATH is not a valid directory, can not archive! " >&2
    return 1
  fi

  local DIR_DNAME=$(basename "$DIR_RPATH")
  local DIR_RPATH=$(dirname "$DIR_RPATH")
  local DIR_DPATH=$(pwd)/$DIR_RPATH

  local ARC_FNAME=$DIR_DNAME.tar.gz
  local ARC_DPATH=${DIR_DPATH/$OLD_ROOT/$NEW_ROOT}

  if [[ "${ARC_DPATH}" = "${DIR_DPATH}" ]]; then
    echo "Failed to mirror path" >@2
    return 1
  fi

  MSG=$(archive ls "${ARC_DPATH}/$ARC_FNAME")
  if [[ ! $MSG == *"cannot access"* ]]; then

    while true; do
      read -r "yn?Folder already archived. Do you want to replace? (y/n) "
      case $yn in
      [Yy]*)
        echo "Re-archiving..."
        break
        ;;
      [Nn]*)
        echo "Exiting..."
        return 0
        ;;
      *) echo "Please answer y or n." ;;
      esac
    done
  fi

  ARGS="-v DIR_DNAME=${DIR_DNAME},DIR_DPATH=${DIR_DPATH},ARC_DPATH=${ARC_DPATH},ARC_FNAME=${ARC_FNAME}"

  qsub "${ARGS}" "${ARC_PBS_PATH}"

}
