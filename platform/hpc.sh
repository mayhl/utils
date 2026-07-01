#!/usr/bin/env sh
# platform/hpc.sh — HPC login node.
#
# SLICE 1 scope: the connectivity SEAM only. Later slices add HPC-only tools
# here: pbs (job scripts), archive (mass storage), swap ($HOME<->$WORKDIR).
#
# On a login node you are already inside the Kerberos realm and use the system
# ssh, so the seam collapses to plain ssh + a no-op auth hook.

# Set authoritatively — the platform module owns the seam.
MU_SSH="$(command -v ssh)"
export MU_SSH

# No kitty on a login node; interactive login uses the same plain ssh.
MU_SSH_LOGIN="$MU_SSH"
export MU_SSH_LOGIN

mu_auth() { :; }
