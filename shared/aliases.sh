#!/usr/bin/env sh
# shared/aliases.sh — portable interactive aliases.

# neovim in place of vim, keeping the original vim reachable as `ovim`.
alias ovim="$(command -v vim)"
alias vim="nvim"

# nvim is the editor everywhere (bash on HPC too) — gives git rebase/commit
# syntax highlighting for free via nvim's gitrebase/gitcommit runtime.
export EDITOR=nvim VISUAL=nvim
