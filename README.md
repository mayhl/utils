# mayhl_utils

A portable shell + Go toolkit to improve workflow on HPC clusters. The same checkout works from an HPC login node or a local macOS/Linux workstation.

> **NOTE:** Works with both bash and zsh. The `mu` engine is a single Go binary (built on first use) — there is no Python or virtualenv.

## Table of Contents
* [Installation](#install)
* [Configuration](#config)
* [Per-node commands](#node)
* [The mu CLI](#mu)
* [SSHFS mounts](#sshfs)
* [Quick Tar](#qtar)
* [HPC info & status](#hpc)
* [Command Summary](#summary)
* [Development](#dev)

## Installation <a name='install'></a>
Clone this repository to a directory of your choice; that path becomes *MU_ROOT*.

    user@host: git clone git@github.com:mayhl/utils.git

Add two lines to your `.zshrc`/`.bashrc` (or an environment file it sources):

    export MU_ROOT=/path/to/utils
    source $MU_ROOT/init.sh

That is the whole hook — no other repository or framework is required. *MU_SYSTEM* (local vs hpc) is **auto-detected** from `$BC_HOST`; set it explicitly only to override.

The `mu` engine is a Go binary at `$MU_ROOT/mu`. It **builds itself on first use** (needs `go` on `PATH`), so a fresh shell just works; on an HPC, drop a cross-compiled `mu` there (`make build-linux`) and no build happens.

Finally, copy the example config and fill it in:

    user@host: cp $MU_ROOT/config.toml.example $MU_ROOT/config.toml
    user@host: $EDITOR $MU_ROOT/config.toml

Open a new shell and run `mu hpc nodes` to confirm everything loaded.

## Configuration <a name='config'></a>
Configuration lives in **`config.toml`** (gitignored — your machine's identity; copy from the tracked `config.toml.example`). The Go engine reads it directly, and `mu shell-init` exports the values the shell layer needs, so it is the single source of truth.

    hpc_user = "your_username"

    [ssh]
    ossh = "/path/to/ossh/bin/ssh"     # a Kerberos ssh build if it's a shell alias (give the path); omit for system ssh

    [sshfs]
    root = "~/hpc_sshfs"               # local parent dir for sshfs mounts

    [[cluster]]
    name   = "alpha"
    domain = "alpha.example.mil"
    nodes  = ["node1", "node2"]

    [[cluster]]
    name   = "beta"
    domain = "beta.example.mil"
    nodes  = ["node3"]

Transfer knobs (`[transfer] rsync_opts`, `ssh_transfer_opts`) have sensible defaults and rarely need changing. Which ssh binary to use is a platform seam handled by the platform module (`MU_SYSTEM` picks `local`/`hpc`); on a workstation a Kerberos ticket is obtained automatically via `pkinit` when needed.

## Per-node commands <a name='node'></a>
`mu shell-init` (run automatically from `init.sh`) generates one dispatcher function per configured node. For a node named `node1`:

| Form | Does |
|------|------|
| `node1` | ssh login (Kerberos handled automatically) |
| `node1 push <local> <remote>` | copy local → node1 |
| `node1 pull <remote> <local>` | copy node1 → local |
| `node1 <cmd> …` | run `<cmd>` on node1 over ssh (a login shell, so modules/`PATH` load) |

For example:

    user@laptop: node1                                   # interactive login
    user@laptop: node1 push ./run42 /p/work/me/run42     # copy up
    user@laptop: node1 pull /p/work/me/run42/out ./out   # copy down
    user@laptop: node1 qstat                             # run a scheduler command remotely

> **NOTE:** Nested tunnels do not work; connect to one HPC at a time.

## The mu CLI <a name='mu'></a>
`mu` is the Go engine. The `push`/`pull` shorthands above are just `mu cp`, which you can also call directly for flags plus a live progress bar and a completion summary:

    user@laptop: mu cp push node1 ./run42 /p/work/me/run42 -n --exclude '*.o'
    user@laptop: mu cp pull node1 /p/work/me/run42/out ./out

Useful options: `--dry-run`/`-n` (preview), `--exclude PATTERN`, `--exclude-hidden` (skip dotfiles/dot-dirs), `--delete`, `--bwlimit RATE`, `-v` (per-file output); add `-h` to any command for help. Top-level commands: `mu cp`, `mu sshfs`, `mu tar`, `mu hpc`, `mu shell-init`.

## SSHFS mounts <a name='sshfs'></a>
Mount an HPC directory locally over sshfs. Register a mount once, then use the `h*` shortcuts:

    user@laptop: mu sshfs add data node1 /p/work/me/data   # register (name → node:path)
    user@laptop: hcd data                                  # mount (if needed) + cd into it
    user@laptop: hls                                       # list mounts with live status
    user@laptop: hset data --ro                            # change node/path or swap ro↔rw (remounts if live)
    user@laptop: hum data                                  # unmount

`hcd`/`hadd`/`hls`/`hset`/`hum` are thin shortcuts over `mu sshfs mount`+`cd` / `add` / `list` / `set` / `umount`. `mu sshfs set` repoints a mount's node/path or swaps read-only ↔ read-write, remounting in place if it is already live. Every filesystem-touching operation is timeout-bounded, and a mount **aborts as soon as sshfs reports a fatal error** (e.g. a missing remote path) instead of hanging — so a bad mount tells you why, fast, rather than freezing the terminal.

## Quick Tar <a name='qtar'></a>
`qtar` (no compression) and `gtar` (gzip) create a tarball from a folder or extract one — the mode is inferred from a `.tar`/`.tar.gz` extension — with a live progress bar:

    user@host: qtar FOLDER              # → FOLDER.tar
    user@host: gtar ARCHIVE.tar.gz      # extract

> **NOTE:** In extraction mode `qtar` and `gtar` are equivalent (compression is auto-detected).

`bqtar` / `bgtar` run their respective commands in the **background at low priority** (`nice`), logging to a `.log` file. All four are thin shims over `mu tar`, which wraps the system `tar` and meters it with the house progress bar.

## HPC info & status <a name='hpc'></a>
`mu hpc` aggregates cross-cluster info — run it from your workstation to reach every cluster:

    user@laptop: mu hpc nodes        # table of configured nodes
    user@laptop: mu hpc nodes -s     # + ssh reachability probe (● up / ○ down)
    user@laptop: mu hpc ticket       # local Kerberos ticket status (--renew runs pkinit)

The shell commands `mu_status` (compact) and `mu_ctx` (per-cluster detail) print environment summaries.

## Command Summary <a name='summary'></a>

The short shell commands are the everyday drivers; where a richer `mu` form exists (progress bar, flags, tab-completion) it is listed alongside. The *Where* column notes whether a command applies on a local workstation, an HPC, or both.

#### Per-node (generated by `mu shell-init`)

| Command | Engine form | Where | Description |
|---------|-------------|-------|-------------|
| `<node>` | — | both | ssh login (Kerberos handled automatically) |
| `<node> push <src> <dst>` | `mu cp push <node> <src> <dst>` | both | copy local → node |
| `<node> pull <src> <dst>` | `mu cp pull <node> <src> <dst>` | both | copy node → local |
| `<node> <cmd>` | — | both | run `<cmd>` on the node over ssh |

#### SSHFS mounts (local)

| Command | Engine form | Where | Description |
|---------|-------------|-------|-------------|
| `hcd <name>` | `mu sshfs mount <name>` + `cd` | local | mount (if needed) and cd in |
| `hadd <name> <node> <path>` | `mu sshfs add` | local | register a mount |
| `hls` | `mu sshfs list` | local | list mounts with live status |
| `hset <name> [--node\|--path\|--ro\|--rw]` | `mu sshfs set` | local | repoint node/path or swap ro↔rw (remounts if live) |
| `hum <name>` | `mu sshfs umount` | local | unmount |

#### Tar

| Command | Engine form | Where | Description |
|---------|-------------|-------|-------------|
| `qtar <dir\|archive>` | `mu tar` | both | create or extract a `.tar` |
| `gtar <dir\|archive>` | `mu tar -z` | both | create or extract a `.tar.gz` |
| `bqtar` / `bgtar` | — | both | background, low-priority `qtar` / `gtar` |

#### HPC info, status & setup

| Command | Where | Description |
|---------|-------|-------------|
| `mu hpc nodes [-s]` | local | node inventory (`-s` = ssh reachability) |
| `mu hpc ticket [--renew]` | local | Kerberos ticket status / obtain via pkinit |
| `mu_status` / `mu_ctx` | both | compact / detailed environment summary |
| `mu shell-init` | both | emit the shell integration (auto-`eval`'d by `init.sh`) |
| `mu_kitty_bootstrap` | local | push kitty terminfo to each configured node |

> **NOTE:** Add `-h` to any `mu` command for help. `node1 push …` and `mu cp push node1 …` do the same transfer — the per-node form is a generated shorthand.

## Development <a name='dev'></a>

The engine is pure Go; the `Makefile` wraps the common tasks.

| Command | Does |
|---------|------|
| `make build` | native `mu` binary for this machine |
| `make build-linux` | static `linux/amd64` binary for HPC deploy |
| `make test` | run the whole suite (`go test ./...`) |
| `make fmt` / `make lint` | gofumpt / golangci-lint |

Running the tests directly gives finer control:

    make test                               # everything, the short way
    go test ./... -race -count=1            # race detector, no test cache
    go test ./internal/shellinit/ -v        # one package, verbose
    go test ./internal/hpc/ -run TestProbe  # one test by name (regex)

> **NOTE:** The suite is hermetic — no cluster, network, or ssh. The per-node dispatcher test drives the generated shell under **both bash and zsh**, skipping a shell that isn't installed (`--- SKIP`), so results are consistent wherever you run it.
