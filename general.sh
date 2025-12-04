#!/usr/bin/env bash

# Aliasing neovim over vim command
# shellcheck disable=SC2139
alias ovim="$(which vim)"
alias vim="nvim"

alias vimc="vim ~/.config/nvim/"

if [ ! -z ${MY_CONFIG_IS_MACOS+x} ]; then

  # cd between terminal quickly using clipboard
  alias cppath='pwd | pbcopy'
  alias cdpath='cd $(pbpaste)'

  # Aliasing MacOS cut with Linux equivlent
  alias cut='/usr/local/bin/gcut'

else
  #echo "WARNING: No quick path clipboarding for Linux"
fi

# Kill all processes via grep matching
function gkill {

  kill -9 $(ps -u $(whoami) | grep "$1" | cut -d ' ' -f4 | xargs)
  #PIDS=$(ps aux | grep "$1" | grep -v grep | awk '{print $2}' | xargs)
  #kill -9 $PIDS
}

# Wrapper around ffmpeg for image series
qffmpeg() {
  IMG_PATH_MASK=$1
  VIDEO_PATH=$2
  FPS=$3

  ffmpeg -r ${FPS} -pattern_type glob -i "$1plot_*.png" -vf "pad=ceil(iw/2)*2:ceil(ih/2)*2" -vcodec libx264 -pix_fmt yuv420p ${VIDEO_PATH}
  #ffmpeg -r ${FPS} -pattern_type glob -i "'${IMG_PATH_MASK}'" -vf "pad=ceil(iw/2)*2:ceil(ih/2)*2"  -vcodec libx264 -pix_fmt yuv420p ${VIDEO_PATH}
}

# Wrapper for quickly taring and compressing by file name
qtar() {
  DIR=$1
  ARCHIVE=${1%/}.tar.gz

  tar -czvf ${ARCHIVE} ${DIR}

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
  git config user.email michael.Y.lam@erdc.dren.mil
}

#############
## Python ##
############

# pyenv for python versioning control on MacOS
if [ ! -z ${MAYHL_UTIL_IS_LOCAL+x} ]; then
  if [ ! -z ${MAYHL_UTIL_IS_MACOS+x} ]; then
    export PYENV_ROOT="$HOME/.pyenv"
    [[ -d $PYENV_ROOT/bin ]] && export PATH="$PYENV_ROOT/bin:$PATH"
    eval "$(pyenv init -)"
  else
    echo "WARNING: No Python versioning control for Linux system"
  fi
fi

# Quick swapping between venvs
alias funtools='source ~/.pyvenvs/funtools/bin/activate'
alias chltools='source ~/.pyvenvs/chltools/bin/activate'
alias wwod='source ~/.pyvenvs/wwod/bin/activate'
