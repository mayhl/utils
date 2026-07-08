# Sandbox test box

A local Docker box standing in for the real HPC login node, so mu's HPC-shaped code
(onboard, cp, sshfs, queue, sync) is testable without a real cluster. Driven by the
`//go:build sandbox` tests in `../sandbox_test.go`.

- **Box:** openSUSE Leap 15.6 (≈ SLES 15), zsh + bash login users (`tester` / `tester-bash`),
  Tcl environment-modules, fake PBS/SLURM stubs in `stubs/`, a mock login banner, and a no-op
  `pkinit` stub in `bin/` (belt-and-suspenders against a real Kerberos prompt).
- **`test-config.toml`** — the laptop-side mu config the tests default to (two clusters share
  the one box: `sandbox` = PBS, `sandslurm` = SLURM). All values are placeholders.

## One-time setup

1. **Throwaway ssh key** (the box is key-auth only). Its **public** half is committed as
   `tester_key.pub`; regenerate and replace it if you don't have the private half:

   ```sh
   ssh-keygen -t ed25519 -N '' -f ~/.ssh/onboard_test_ed25519
   cp ~/.ssh/onboard_test_ed25519.pub internal/integration/sandbox/tester_key.pub
   ```

2. **`~/.ssh/config` aliases** (host → container on `localhost:2222`). Do **not** set
   `LogLevel ERROR` here — it hides the box's banner, which `TestSSHBannerQuieted` relies on
   being visible to prove mu passes `ssh -q`. mu quiets the banner itself.

   ```
   Host sandbox sandbox-bash sandbox.local sandslurm.local
       HostName localhost
       Port 2222
       IdentityFile ~/.ssh/onboard_test_ed25519
       StrictHostKeyChecking no
       UserKnownHostsFile /dev/null
   Host sandbox sandbox.local sandslurm.local
       User tester
   Host sandbox-bash
       User tester-bash
   ```

## Run

```sh
# from this directory: bring the box up / rebuild after editing the Dockerfile
docker compose up -d --build

# from the repo root: run the suite (defaults to ./internal/integration/sandbox/test-config.toml;
# skips cleanly if the box is unreachable). Override the config with MU_SANDBOX_CONFIG.
go test -tags sandbox ./internal/integration/

docker compose down   # tear the box down
```

> **Kerberos safety:** the tests set `MU_SYSTEM=hpc` so mu never runs `pkinit` against a real
> realm (the box is key-auth). Never remove that guard from the harness.
