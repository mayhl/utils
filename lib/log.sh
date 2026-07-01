#!/usr/bin/env sh
# lib/log.sh — standardized logging. Canonical home for mu_log.
# Usage: mu_log <INFO|WARN|ERROR> "Message"

mu_log() {
  local level msg log_file
  level=$1
  msg=$2
  log_file="${HOME}/.cache/mayhl_utils/framework.log"
  printf "[%s] [%-5s] %s\n" "$(date +%Y-%m-%dT%H:%M:%S)" "$level" "$msg" >> "$log_file"

  # Errors also go to stderr, in red.
  if [ "$level" = "ERROR" ]; then
    printf "\033[0;31m[ERROR]\033[0m %s\n" "$msg" >&2
  fi
}
