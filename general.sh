#!/usr/bin/env bash

# Aliasing neovim over vim command
# shellcheck disable=SC2139
alias ovim="$(which vim)"
alias vim="source $HOME/.pyvenvs/nvim/bin/activate && nvim"

alias vimc="vim ~/.config/nvim/"

if def MAYHL_UTILS_IS_MACOS; then
  alias cut='/usr/local/bin/gcut'
fi

# Aliasing MacOS cut with Linux equivlent
alias cppath='pwd | clipcopy'
alias cdpath='cd $(clippaste)'

# Kill all processes via grep matching
function gkill {
  kill -9 $(pgrep "$1" | xargs)
}

# Wrapper around ffmpeg for image series
function qffmpeg {
  IMG_PATH_MASK=$1
  VIDEO_PATH=$2
  FPS=$3

  ffmpeg -r ${FPS} -pattern_type glob -i "$1plot_*.png" -vf "pad=ceil(iw/2)*2:ceil(ih/2)*2" -vcodec libx264 -pix_fmt yuv420p ${VIDEO_PATH}
  #ffmpeg -r ${FPS} -pattern_type glob -i "'${IMG_PATH_MASK}'" -vf "pad=ceil(iw/2)*2:ceil(ih/2)*2"  -vcodec libx264 -pix_fmt yuv420p ${VIDEO_PATH}
}

#######
# Git #
#######

# Commands for switching between git accounts
setGitHubUsr() {
  git config user.name mayhl
  git config user.email michaelangelo.yh.lam@gmail.com
}

setGitLabUsr() {
  git config user.name 'Lam, Michael-Angelo Y'
  git config user.email michaelangelo.yh.lam@gmail.com
}

#############
## Python ##
############

# pyenv for python versioning control on MacOS
if def MAYHL_UTILS_IS_LOCAL; then
  if def MAYHL_UTILS_IS_MACOS; then
    export PYENV_ROOT="$HOME/.pyenv"
    [[ -d $PYENV_ROOT/bin ]] && export PATH="$PYENV_ROOT/bin:$PATH"
    eval "$(pyenv init - zsh)"
  else
    echo "WARNING: No Python versioning control for Linux system"
  fi
fi

# Quick swapping between venvs
alias funtools='source ~/.pyvenvs/funtools/bin/activate'
alias chltools='source ~/.pyvenvs/chltools/bin/activate'
alias wwod='source ~/.pyvenvs/wwod/bin/activate'

swap_rec() {

  DEMO_P10K="${HOME}/.config/oh_my_zsh/p10k_config_asciinema.zsh"

  if [[ -v _RECORD_FLAG ]]; then
    P10K_CONFIG=$_OLD_P10K_CONFIG
    source "$P10K_CONFIG"
    unset _RECORD_FLAG
    unset _OLD_P10K_CONFIG

  else
    _OLD_P10K_CONFIG="$P10K_CONFIG"
    P10K_CONFIG=$DEMO_P10K
    source "$DEMO_P10K"
    export _RECORD_FLAG="T"
  fi

}

spinner() {

  sh ${HOME}/repos/mayhl_utils/spinners.sh "${@}"

}

record() {

  if [[ "${1:e}" ]]; then
    name="${1:r}"
  else
    name="$1"
  fi

  if [[ ! -v _RECORD_FLAG ]]; then
    echo "Run swap_rec first. Exiting!!!"
    return 1
  fi

  asciinema rec "${name}.cast" "${@:2}"
  #spinner run dots3 "Converting to SVG" cyan \
  asciinema convert -f asciicast-v2 "${name}.cast" "${name}.v2.cast" || return 1

  #grep "exit" "${name}.cast" -n | tail -n 1 | cut -d : -f1

  line_num=$(grep "START RECORDING" "${name}.v2.cast" -n | tail -n 1 | cut -d : -f1)
  if [[ $line_num > 0 ]]; then
    head -n 1 "${name}.v2.cast" >"${name}.v3.cast"
    sed "1,${line_num}d" "${name}.v2.cast" >>"${name}.v3.cast"
    mv -f "${name}.v3.cast" "${name}.v2.cast"
  fi

  line_num=$(grep "\^D" "${name}.v2.cast" -n | tail -n 1 | cut -d : -f1)
  if [[ $line_num > 0 ]]; then
    line_num=$((line_num - 1))
    head -n ${line_num} "${name}.v2.cast" >"${name}.cast"
    rm "${name}.v2.cast"
  else
    mv -f "${name}.v2.cast" "${name}.cast"
  fi

  cat "${name}.cast" | npx svg-term-cli --out "${name}.svg" --window
}

crop_record() {

  name=${1:r}
  orig=$1
  new="${name}.v2.cast"
  tmp="${name}.tmp.cast"

  cp "${orig}" "${new}"

  line_num=$(grep "START_RECORDING" "${new}" -n | tail -n 1 | cut -d : -f1)

  echo "line_num: $line_num"
  if [[ $line_num > 0 ]]; then
    echo "HERE"
    head -n 1 "${new}" >"${tmp}"
    sed "1,${line_num}d" "${new}" >>"${tmp}"
    mv -f "${tmp}" "${new}"
  fi

  # line_num=$(grep "\^D" "${new}" -n | tail -n 1 | cut -d : -f1)
  # if [[ $line_num > 0 ]]; then
  #   line_num=$((line_num - 1))
  #   head -n ${line_num} "${new}" >"${tmp}"
  #   mv -f "${tmp}" "${new}"
  # fi

}

cast2svg() {

  cat "$1" | npx svg-term-cli --out "${1:r}.svg" --window

}
# dummy

# Utility Functions
mytb() {
  local logfile="/tmp/$$.log"
  if ! command -v tbvaccine >/dev/null 2>&1 || ! command -v rcat >/dev/null 2>&1; then
    echo "mytb: Required tools missing."
    "$@"
    return $?
  fi
  "$@" >"$logfile" 2>&1
  if [ $? -ne 0 ]; then
    cat "$logfile" | tbvaccine
  else
    rcat "$logfile"
  fi
  rm -f "$logfile"
}

# Unified Execution Wrapper: Logs command, runs it, cleans output, and logs status
# Usage: mu_run <command> [args...]
mu_run() {
  local _log_file="${HOME}/.cache/mayhl_utils/audit.log"
  local _cmd_str="$*"
  local _timestamp="$(date +%Y-%m-%dT%H:%M:%S)"

  # Log the start of the command
  printf "[%s] [EXEC] %s\n" "$_timestamp" "$_cmd_str" >>"$_log_file"

  # Execute and pipe through tbvaccine if available
  if command -v tbvaccine >/dev/null 2>&1; then
    eval "$_cmd_str" 2>&1 | tbvaccine >>"$_log_file" 2>&1
  else
    eval "$_cmd_str" >>"$_log_file" 2>&1
  fi

  local _status=${PIPESTATUS[0]}

  if [ $_status -ne 0 ]; then
    printf "[%s] [FAIL] Status: %s | Cmd: %s\n" "$_timestamp" "$_status" "$_cmd_str" >>"$_log_file"
  else
    printf "[%s] [OK] Cmd: %s\n" "$_timestamp" "$_cmd_str" >>"$_log_file"
  fi

  return $_status
}

# Display Mayhl Utils environment status
mu_status() {
  echo "--- Mayhl Utils Status ---"
  echo "System:   $MU_SYSTEM"
  echo "Path:     $MU_PATH"
  echo "Git Hash: $(git -C "$MU_PATH" rev-parse --short HEAD 2>/dev/null || echo 'Not a git repo')"
  echo "Clusters: $(compgen -v | grep '^MU_.*_HOST$' | sed 's/_HOST//g' | tr '\n' ' ')"
}
