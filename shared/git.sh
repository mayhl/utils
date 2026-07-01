#!/usr/bin/env sh
# shared/git.sh — switch the local git author identity between accounts.

setGitHubUsr() {
  git config user.name mayhl
  git config user.email michaelangelo.yh.lam@gmail.com
}

setGitLabUsr() {
  git config user.name 'Lam, Michael-Angelo Y'
  git config user.email michaelangelo.yh.lam@gmail.com
}
