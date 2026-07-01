#!/usr/bin/env sh
# shared/aliases.sh — portable interactive aliases.

# neovim in place of vim, keeping the original vim reachable as `ovim`.
alias ovim="$(command -v vim)"
alias vim="nvim"
alias vimc="vim ~/.config/nvim/"
