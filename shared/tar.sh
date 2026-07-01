#!/usr/bin/env sh
# shared/tar.sh — progress-bar tar/gzip helpers.
#
# Uses tqdm for a live progress bar when it's importable (optionally from the
# declared venv $MU_PY_VENV); otherwise falls back to plain verbose tar.
#
#   qtar <dir>          -> dir.tar          (no compression)
#   qtar <file.tar>     -> extract
#   gtar <dir>          -> dir.tar.gz       (gzip)
#   gtar <file.tar.gz>  -> extract
#   bqtar / bgtar       background + reniced variants (log to <name>.qtar.log)

# tqdm probe, run in a subshell so any venv activation stays local and does not
# leak into the interactive shell at load time.
_mu_have_tqdm() (
  if [ -n "${MU_PY_VENV}" ] && [ -f "${MU_PY_VENV}/bin/activate" ]; then
    # shellcheck source=/dev/null
    . "${MU_PY_VENV}/bin/activate"
  fi
  "${MU_HPC_PYTHON:-python}" -c "import tqdm" 2> /dev/null
)

# Activate the tqdm venv if one is configured (call-site paired with deactivate).
_mu_tqdm_activate() {
  [ -n "${MU_PY_VENV}" ] && [ -f "${MU_PY_VENV}/bin/activate" ] || return 0
  # shellcheck source=/dev/null
  . "${MU_PY_VENV}/bin/activate"
}
_mu_tqdm_deactivate() { command -v deactivate > /dev/null 2>&1 && deactivate; }

if _mu_have_tqdm; then

  gtar() {
    if [ "${1%.tar.gz}" != "$1" ]; then
      _mu_tqdm_activate
      tar -v -xzf "$1" -C . | tqdm --desc "Files" --total "$(tar -tzf "$1" | wc -l)" > /dev/null
      _mu_tqdm_deactivate
    elif [ "${1%.tar}" != "$1" ]; then
      qtar "$1"
    else
      local dir=$1 archive=${1%/}.tar.gz size
      _mu_tqdm_activate
      size=$(mu_du_bytes "$dir")
      tar -cf - "$dir" |
        tqdm --bytes --total "$size" --desc "Processing" |
        gzip |
        tqdm --bytes --total "$size" --desc "Compressed" --position 1 > "$archive"
      _mu_tqdm_deactivate
    fi
  }

  qtar() {
    if [ "${1%.tar.gz}" != "$1" ]; then
      gtar "$1"
    elif [ "${1%.tar}" != "$1" ]; then
      _mu_tqdm_activate
      tar -v -xf "$1" -C . | tqdm --desc "Files" --total "$(tar -tf "$1" | wc -l)" > /dev/null
      _mu_tqdm_deactivate
    else
      local dir=$1 archive=${1%/}.tar size
      _mu_tqdm_activate
      size=$(mu_du_bytes "$dir")
      tar -cf - "$dir" | tqdm --bytes --total "$size" > "$archive"
      _mu_tqdm_deactivate
    fi
  }

else

  gtar() {
    if [ "${1%.tar.gz}" != "$1" ]; then
      tar -vxzf "$1"
    elif [ "${1%.tar}" != "$1" ]; then
      qtar "$1"
    else
      tar -czvf "${1%/}.tar.gz" "$1"
    fi
  }

  qtar() {
    if [ "${1%.tar.gz}" != "$1" ]; then
      gtar "$1"
    elif [ "${1%.tar}" != "$1" ]; then
      tar -xvf "$1"
    else
      tar -cvf "${1%/}.tar" "$1"
    fi
  }

fi

bqtar() {
  local log=${1%/}.qtar.log
  qtar "$1" > "$log" 2>&1 &
  renice -n 20 -p $! > /dev/null 2>&1
}

bgtar() {
  local log=${1%/}.gtar.log
  gtar "$1" > "$log" 2>&1 &
  renice -n 20 -p $! > /dev/null 2>&1
}
