#!/usr/bin/env sh
# shared/tar.sh — thin shims over `mu tar` (the Go engine owns the progress bar).
#
#   qtar <dir>          -> dir.tar          (or extract a .tar / .tar.gz)
#   gtar <dir>          -> dir.tar.gz       (or extract)
#   bqtar / bgtar       background + reniced variants (log to <name>.qtar/gtar.log)
#
# The verb (create vs extract) is inferred from the path by `mu tar`.

qtar() { mu tar "$@"; }
gtar() { mu tar -z "$@"; }

bqtar() {
  log="${1%/}.qtar.log"
  mu tar "$1" > "$log" 2>&1 &
  renice -n 20 -p $! > /dev/null 2>&1
}

bgtar() {
  log="${1%/}.gtar.log"
  mu tar -z "$1" > "$log" 2>&1 &
  renice -n 20 -p $! > /dev/null 2>&1
}
