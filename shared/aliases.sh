#!/usr/bin/env sh
# shared/aliases.sh — portable interactive aliases.

# neovim in place of vim, keeping the original vim reachable as `ovim`.
alias ovim="$(command -v vim)"
alias vim="nvim"

# nvim is the editor everywhere (bash on HPC too) — gives git rebase/commit
# syntax highlighting for free via nvim's gitrebase/gitcommit runtime.
export EDITOR=nvim VISUAL=nvim

# mu front-doors (portable — mu ps/log work on the laptop and HPC alike). The m-prefix
# mirrors the queue family (mstat/minfo/…); aliases forward args, so `mps -i` still hits
# the interactive picker.
alias mps='mu ps'    # local processes, optionally name-masked; mps -i = picker
alias mlog='mu log'  # event log (transfers, jobs, big ops)
