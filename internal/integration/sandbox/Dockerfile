# Onboard test box — public stand-in for the real HPC login node.
#   Real: SLES 15 (SP5/SP7), zsh login, proprietary PE Tcl modules 3.2.11.x.
#   Here: openSUSE Leap 15.6 (same sources as SLE 15.x), zsh login, public Tcl
#   environment-modules (compatible modulefile format; different version — fine for
#   testing modulefile create/load, not a byte-exact PE clone).
# Used to test onboard.sh (push mu + .config to a fresh box) without a real cluster.
# Two users so both rc paths are exercised: tester (zsh, matches real) + tester-bash.
FROM opensuse/leap:15.6

RUN zypper --non-interactive --gpg-auto-import-keys refresh && \
    zypper --non-interactive install -y \
      openssh zsh bash tar gzip gawk coreutils shadow which less rsync git \
      environment-modules tcl && \
    zypper clean -a

# sshd: host keys + privsep dirs; key-only auth (no passwords)
RUN ssh-keygen -A && mkdir -p /var/lib/sshd /run/sshd && \
    sed -ri 's/^#?PasswordAuthentication .*/PasswordAuthentication no/'  /etc/ssh/sshd_config && \
    sed -ri 's/^#?PubkeyAuthentication .*/PubkeyAuthentication yes/'     /etc/ssh/sshd_config

# Mock pre-auth login banner — a generic stand-in for the noisy multi-line banner a real
# login node prints. Exists so tests can prove mu's ssh calls pass -q and never leak the
# banner into captured/forwarded output. Shown on every connection incl. `ssh host cmd`. The
# MU-MOCK-BANNER tag is the test's match anchor.
RUN printf '%s\n' \
    '===============================================================' \
    '  MU-MOCK-BANNER — sandbox test box (this is a mock banner).' \
    '  Authorized use only. Activity may be monitored.' \
    '  Disconnect now if you are not an authorized user.' \
    '===============================================================' > /etc/ssh/banner.txt && \
    echo 'Banner /etc/ssh/banner.txt' >> /etc/ssh/sshd_config

# Two test users: zsh (matches the real /bin/zsh node) and bash — same key.
RUN useradd -m -s /bin/zsh tester && useradd -m -s /bin/bash tester-bash
COPY tester_key.pub /tmp/tester_key.pub
RUN for u in tester tester-bash; do \
      mkdir -p /home/$u/.ssh && \
      cp /tmp/tester_key.pub /home/$u/.ssh/authorized_keys && \
      chown -R $u /home/$u/.ssh && \
      chmod 700 /home/$u/.ssh && chmod 600 /home/$u/.ssh/authorized_keys; \
    done && rm -f /tmp/tester_key.pub

# Make `module` resolvable in zsh login shells (bash gets it via /etc/profile.d
# automatically). Best-effort PE-init stand-in; never fails the build.
RUN sh -c 'for p in /usr/share/modules/init/zsh /usr/share/Modules/init/zsh; do \
             [ -f "$p" ] && echo ". $p" >> /etc/zprofile; done; true'

# Tier-1 fake schedulers (PBS qstat/qdel + SLURM squeue/sacct/scontrol/scancel) plus the
# show_queues site wrapper on PATH — emit realistic output so mu's queue read-side
# (mstat/minfo/mhist/queues) parses + renders without real daemons. Swap for real
# slurmctld/pbs_server for write-side later.
COPY stubs/ /usr/local/bin/
RUN chmod +x /usr/local/bin/qstat /usr/local/bin/qdel /usr/local/bin/squeue \
             /usr/local/bin/sacct /usr/local/bin/scontrol /usr/local/bin/scancel \
             /usr/local/bin/show_queues

EXPOSE 22
CMD ["/usr/sbin/sshd","-D","-e"]
