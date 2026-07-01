#!/usr/bin/env sh
# lib/log.sh — standardized logging + message wrapping.
#
# mu_log <INFO|WARN|ERROR|OK> "message"
#   - always appends a plain, single-line record to the framework log
#   - also prints to the terminal: INFO/OK -> stdout, WARN/ERROR -> stderr,
#     with a colored, fixed-width [LABEL] and hanging-indent wrapping
#   - color is emitted only when the target stream is a TTY; piped/redirected
#     output and the logfile stay plain and grep-friendly

# Visible width of a string, ignoring ANSI SGR escapes. The trailing 'X'
# sentinel protects any trailing spaces from command-substitution stripping.
_mu_visible_len() {
  local s esc
  esc=$(printf '\033')
  s=$(printf '%sX' "$1" | sed "s/${esc}\[[0-9;]*m//g")
  s=${s%X}
  printf '%s' "${#s}"
}

# mu_wrap <prefix> <message...>
# Print prefix + message wrapped to terminal width, continuation lines
# hang-indented to the prefix's visible width. Prefix may contain color codes.
mu_wrap() {
  local prefix=$1
  shift
  local msg=$* cols plen twidth indent
  cols=${COLUMNS:-$(tput cols 2> /dev/null || echo 80)}
  # guard against unset/zero/non-numeric COLUMNS (non-interactive shells)
  case $cols in '' | *[!0-9]* | 0) cols=80 ;; esac
  plen=$(_mu_visible_len "$prefix")
  twidth=$((cols - plen))
  [ "$twidth" -lt 20 ] && twidth=20
  indent=$(printf '%*s' "$plen" '')
  # `|| [ -n "$line" ]` processes the final line, which fold emits without a
  # trailing newline (a plain `while read` would silently drop it).
  printf '%s' "$msg" | fold -s -w "$twidth" | {
    IFS= read -r line
    printf '%s%s\n' "$prefix" "$line"
    while IFS= read -r line || [ -n "$line" ]; do printf '%s%s\n' "$indent" "$line"; done
  }
}

mu_log() {
  local level=$1
  shift
  local msg=$*
  local logfile="${HOME}/.cache/mayhl_utils/framework.log"
  local esc reset color stream lab prefix

  # always: plain, single line to the logfile (padded for column alignment)
  printf '[%s] [%-5s] %s\n' "$(date +%Y-%m-%dT%H:%M:%S)" "$level" "$msg" >> "$logfile"

  esc=$(printf '\033')
  reset="${esc}[0m"
  case $level in
    INFO)
      color="${esc}[36m"
      stream=1
      ;; # cyan
    OK)
      color="${esc}[32m"
      stream=1
      ;; # green
    WARN)
      color="${esc}[33m"
      stream=2
      ;; # yellow
    ERROR)
      color="${esc}[31m"
      stream=2
      ;; # red
    *)
      color=''
      stream=1
      ;;
  esac

  # gate color on the target stream being a terminal
  if [ "$stream" = 1 ]; then [ -t 1 ] || color=''; else [ -t 2 ] || color=''; fi

  # fixed-width label (pad to 5 = width of ERROR); direct assignment keeps
  # the trailing space that command substitution would strip.
  lab=$level
  while [ ${#lab} -lt 5 ]; do lab="$lab "; done
  if [ -n "$color" ]; then
    prefix="${color}[${lab}]${reset} "
  else
    prefix="[${lab}] "
  fi

  if [ "$stream" = 1 ]; then
    mu_wrap "$prefix" "$msg"
  else
    mu_wrap "$prefix" "$msg" >&2
  fi
}
