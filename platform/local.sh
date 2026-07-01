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

# auth hook: obtain a Kerberos ticket if none is present for the HPC user.
mu_auth() {
  if ! klist 2> /dev/null | grep -q "${MU_HPC_UNAME}"; then
    echo "No Kerberos ticket for ${MU_HPC_UNAME}; running pkinit..."
    pkinit "${MU_HPC_UNAME}"
  fi
}
