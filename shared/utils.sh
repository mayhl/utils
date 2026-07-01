#!/usr/bin/env sh
# shared/utils.sh — small portable helpers.

# rich spinner around a command:  spinner "<message>" <command> [args...]
# Falls back to running the command plainly if the Python venv isn't set up.
spinner() {
  if [ -x "${MU_PY_VENV}/bin/python" ]; then
    mu_py "${MU_ROOT}/lib/py/spinner.py" "$@"
  else
    shift 2> /dev/null
    "$@"
  fi
}

# kill -9 every process whose name matches a pattern
gkill() {
  local pids
  pids=$(pgrep "$1")
  if [ -n "$pids" ]; then
    # shellcheck disable=SC2086
    kill -9 $pids
  else
    echo "gkill: no process matching '$1'"
  fi
}

# encode an image series (<mask>plot_*.png) to mp4 (needs ffmpeg)
qffmpeg() {
  mu_have ffmpeg || { mu_log "ERROR" "qffmpeg: ffmpeg not found"; return 1; }
  local mask=$1 out=$2 fps=$3
  ffmpeg -r "${fps}" -pattern_type glob -i "${mask}plot_*.png" \
    -vf "pad=ceil(iw/2)*2:ceil(ih/2)*2" -vcodec libx264 -pix_fmt yuv420p "${out}"
}

# run a command, prettifying a failing traceback (needs tbvaccine + rcat)
mytb() {
  local logfile="/tmp/mu_$$.log" rc
  if ! mu_have tbvaccine || ! mu_have rcat; then
    "$@"
    return $?
  fi
  "$@" > "$logfile" 2>&1
  rc=$?
  if [ "$rc" -ne 0 ]; then tbvaccine < "$logfile"; else rcat "$logfile"; fi
  rm -f "$logfile"
  return "$rc"
}

# log + run a command, capturing its status portably
# (bash exposes PIPESTATUS; zsh exposes the 1-indexed pipestatus)
mu_run() {
  local logfile="${HOME}/.cache/mayhl_utils/audit.log"
  local cmd="$*" ts status
  ts=$(date +%Y-%m-%dT%H:%M:%S)
  printf '[%s] [EXEC] %s\n' "$ts" "$cmd" >> "$logfile"
  if mu_have tbvaccine; then
    eval "$cmd" 2>&1 | tbvaccine >> "$logfile" 2>&1
    if [ -n "$ZSH_VERSION" ]; then status=${pipestatus[1]}; else status=${PIPESTATUS[0]}; fi
  else
    eval "$cmd" >> "$logfile" 2>&1
    status=$?
  fi
  if [ "$status" -ne 0 ]; then
    printf '[%s] [FAIL] status=%s cmd=%s\n' "$ts" "$status" "$cmd" >> "$logfile"
  else
    printf '[%s] [OK]   cmd=%s\n' "$ts" "$cmd" >> "$logfile"
  fi
  return "$status"
}
