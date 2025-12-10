#!/usr/bin/env bash

# Aliasing neovim over vim command
# shellcheck disable=SC2139
alias ovim="$(which vim)"
alias vim="nvim"

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
