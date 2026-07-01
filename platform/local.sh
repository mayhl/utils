#!/usr/bin/env sh
# platform/local.sh — LOCAL workstation (e.g. macOS laptop).
#
# SLICE 1 scope: the connectivity SEAM only (MU_SSH + mu_auth + Kerberos PATH).
# Later slices add the local-only tools here: sshfs mount/hcd, pyenv, kitty was
# dropped. connect.sh consumes MU_SSH/mu_auth and never branches on platform.

# Kerberos + GSSAPI-enabled OpenSSH build used to reach the DoD HPCs.
# Prepend only once so re-sourcing .zshrc does not bloat PATH.
case ":$PATH:" in
  *:/usr/local/ossh/bin:*) ;;
  *) export PATH="/usr/local/krb5/bin:/usr/local/ossh/bin:$PATH" ;;
esac
export KRB5_CONFIG="${KRB5_CONFIG:-/etc/krb5.conf}"

# ssh binary: prefer the Kerberos `ossh` build, fall back to system ssh.
# Set authoritatively — the platform module owns the seam (a stale MU_SSH from
# the environment must not win).
MU_SSH="$(command -v ossh || command -v ssh)"
export MU_SSH

# Interactive-login ssh: inside kitty, wrap with the ssh kitten (shell
# integration / clipboard / terminfo over ssh). It execs the `ssh` on PATH,
# which is the ossh build, so Kerberos is preserved. File transfers (rsync -e)
# keep the plain MU_SSH — the kitten's session setup is pointless there.
if [ -n "${KITTY_WINDOW_ID}" ] && mu_have kitty; then
  MU_SSH_LOGIN="kitty +kitten ssh"
else
  MU_SSH_LOGIN="$MU_SSH"
fi
export MU_SSH_LOGIN

# auth hook: obtain a Kerberos ticket if none is present for the HPC user.
mu_auth() {
  if ! klist 2> /dev/null | grep -q "${MU_HPC_UNAME}"; then
    mu_log "INFO" "No Kerberos ticket for ${MU_HPC_UNAME}; running pkinit..."
    pkinit "${MU_HPC_UNAME}"
  fi
}

# pyenv (Python version management) when installed.
if mu_is_macos; then
  export PYENV_ROOT="${HOME}/.pyenv"
  [ -d "${PYENV_ROOT}/bin" ] && export PATH="${PYENV_ROOT}/bin:$PATH"
  mu_have pyenv && eval "$(pyenv init -)"
fi

# One-time-per-HPC setup: push kitty's terminfo to the HPC systems so kitty
# renders correctly over ssh (parallels mu_py_bootstrap). Local-only (kitty is
# your terminal). One push per node — each Alpha system has its own $HOME.
# Driven by MU_CLUSTERS.
mu_kitty_bootstrap() {
  mu_have kitty || {
    mu_log "ERROR" "kitty not found"
    return 1
  }
  mu_auth
  local c cu domain nodes node target
  for c in $(echo "$MU_CLUSTERS"); do
    cu=$(printf '%s' "$c" | tr '[:lower:]' '[:upper:]')
    domain=$(mu_indirect "MU_CLUSTER_${cu}_DOMAIN")
    nodes=$(mu_indirect "MU_CLUSTER_${cu}_NODES")
    [ -n "$domain" ] || continue
    for node in $(echo "$nodes"); do
      target="${MU_HPC_UNAME}@${node}.${domain}"
      mu_log "INFO" "kitty terminfo -> ${target}"
      if timeout 15 kitty +kitten ssh "$target" true; then
        mu_log "OK" "${node}: terminfo updated"
      else
        mu_log "ERROR" "${node}: terminfo push failed"
      fi
    done
  done
}
