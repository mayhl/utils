# mayhl_utils

A portable shell toolkit to improve workflow on HPC clusters. The same checkout works from an HPC login node or a local macOS/Linux workstation.

> **NOTE:** Works with both bash and zsh. The *mu* CLI and the tar progress bars use Python 3 through a self-contained virtual environment — no system installs.

## Table of Contents
* [Installation](#install)
* [Configuration](#config)
* [SSH to an HPC](#ssh)
* [Copying Files Between HPCs](#rsync)
* [The mu CLI](#mu)
* [Quick Tar](#qtar)
* [Status](#status)
* [Command Summary](#summary)

## Installation <a name='install'></a>
To install, first clone this repository to a directory of your choice. This path becomes *MU_ROOT*.

    user@hpc: cd path/of/your/choice
    user@hpc: git clone git@github.com:mayhl/utils.git

The toolkit needs only two coordinates and a source line — no other repository or framework is required. Add the following to your `.zshrc` or `.bashrc` (or a personal environment file it sources):

    export MU_ROOT=path/of/your/choice/utils
    export MU_SYSTEM=local
    source $MU_ROOT/init.sh

*MU_SYSTEM* selects the platform module: `local` for a workstation, `hpc` for a login node.

> **NOTE:** *MU_SYSTEM* only picks the platform module (local vs hpc); it does not imply an operating system, and defaults to *local* if unset.

Next, copy the example config and fill in your details:

    user@hpc: cp $MU_ROOT/config.env.example $MU_ROOT/config.env
    user@hpc: vim $MU_ROOT/config.env

Finally, build the Python virtual environment used by the *mu* CLI and the tar progress bars:

    user@hpc: mu_py_bootstrap

Open a new shell (or re-source your rc file) and run *mu_status* to confirm everything loaded.

## Configuration <a name='config'></a>
Configuration is split across two files, sourced in order: *defaults.env* (tracked, shared knobs) and *config.env* (gitignored, your machine's identity). Only *config.env* needs editing — copy it from *config.env.example*.

In *config.env*, set your HPC login name and list the clusters to generate aliases for. Each cluster needs a matching *_DOMAIN* and *_NODES* variable keyed by its ALL-CAPS name, e.g.

    export MU_HPC_UNAME=your_username
    export MU_CLUSTERS="alpha"
    export MU_CLUSTER_ALPHA_DOMAIN="alpha.example.mil"
    export MU_CLUSTER_ALPHA_NODES="node1 node2"

> **NOTE:** Cluster names in *MU_CLUSTERS* are lowercase; the per-cluster variables use the ALL-CAPS name.

Behavior knobs live in *defaults.env* and rarely need changing: the rsync options (*MU_HPC_RSYNC_OPTS*, default `-avuP`), the ssh options (*MU_HPC_SSH_OPTS*, default `-Y`), the Python interpreter (*MU_PYTHON*), and the venv location (*MU_PY_VENV*, default `~/.cache/mayhl_utils/venv`). Override any of them in *config.env* if needed.

The generated ssh and copy aliases are cached and regenerate automatically when *connect.sh* or *config.env* change. To force a refresh, run *mu_connect_refresh*.

## SSH to an HPC <a name='ssh'></a>
An alias is created for each configured node — simply type its name to connect. On a local workstation, a Kerberos ticket is obtained automatically (via *pkinit*) if you do not already have one, e.g.

    user@laptop: node1

> **NOTE:** Nested tunnels do not work; connect to one HPC at a time.

## Copying Files Between HPCs <a name='rsync'></a>
Files are copied via [rsync](https://rsync.samba.org/). Two commands are created for each node: *cpName*, copy from node 'Name' to the current machine; and *cp2Name*, copy to node 'Name' from the current machine (the *2* is a mnemonic for "to"). For example,

    user@laptop: cpNode1 /p/work/me/run42/out ./out
    user@laptop: cp2Node1 ./case.in /p/home/me/case

Both authenticate automatically and stream rsync's output (and any ssh prompts) straight to the terminal.

## The mu CLI <a name='mu'></a>
For richer, flag-driven transfers, the *mu* command (a Typer CLI backed by the venv) provides the same copies with a live progress bar, dry-run, and filters:

    user@laptop: mu cp push node1 ./run42 /p/work/me/run42
    user@laptop: mu cp pull node1 /p/work/me/run42/out ./out
    user@laptop: mu cp nodes

*mu cp push* copies local to node, *mu cp pull* copies node to local, and *mu cp nodes* prints a table of the configured nodes. The node may be a bare name (which tab-completes) or an explicit user@host. Useful options include *--dry-run* (*-n*) to preview, *--exclude PATTERN* to skip files, *--delete* to remove extraneous files at the destination, and *--bwlimit RATE* to cap bandwidth. Add *-h* to any command for help. For example,

    user@laptop: mu cp push node1 ./run42 /p/work/me/run42 -n --exclude '*.o'

## Quick Tar <a name='qtar'></a>
To quickly put a folder into a tarball or extract the files from a tarball (with or without gzip compression), use the wrapper commands *qtar* (no compression) and *gtar* (with compression). The commands check for a *.tar* or *.tar.gz* file extension to determine whether to run in extract mode. For example,

    user@hpc: qtar FOLDER
    user@hpc: gtar ARCHIVE.tar.gz

> **NOTE:** In extraction mode *gtar* and *qtar* are equivalent.

#### Background Mode
For larger folders/archives, the commands *bqtar* and *bgtar* execute their respective commands in the background at low priority. Output is piped to a *.log* file.

> **NOTE:** Low-priority mode applies the *nice* command.

#### Python & tqdm
If the Python package [tqdm](https://tqdm.github.io/) is available (it is installed into the toolkit venv by *mu_py_bootstrap*), *qtar* and *gtar* provide progress bars. An example of the extended output is below,

    user@hpc: gtar FOLDER
    Processing:  31%|███            |  112M/365M [00:05<00:15, 17.2MB/s]
    Compressed:   7%|█              | 24.8M/365M [00:05<01:21, 4.38MB/s]

> **NOTE:** The Compressed progress bar will not fill up but gives the size of the compressed archive.

## Status <a name='status'></a>
The command *mu_status* prints a compact summary (system, root, git revision, ssh binary, user, clusters), and *mu_ctx* prints a detailed per-cluster listing of domains and nodes.

## Command Summary <a name='summary'></a>

The short shell commands are the everyday drivers; where a richer Typer form
exists (progress bar, flags, tab-completion), it is listed alongside. The *Where*
column notes whether a command applies on a local workstation, an HPC, or both.

#### Bootstrap & Init

| Command | Typer equivalent | Where | Description |
|---------|------------------|-------|-------------|
| `mu_py_bootstrap` | — | both | create or refresh the Python venv |
| `mu_connect_refresh` | — | both | regenerate the ssh/copy aliases |
| `mu_kitty_bootstrap` | — | local | push kitty terminfo to each configured node |

#### HPC — Connectivity & Transfers

| Command | Typer equivalent | Where | Description |
|---------|------------------|-------|-------------|
| `<node>` | — | both | ssh to a configured node (Kerberos handled automatically) |
| `cp2<Node> <src> <dst>` | `mu cp push <node> <src> <dst>` | both | copy *to* the node (here → node) |
| `cp<Node> <src> <dst>` | `mu cp pull <node> <src> <dst>` | both | copy *from* the node (node → here) |
| — | `mu cp nodes` | both | list the configured nodes |

#### General

| Command | Typer equivalent | Where | Description |
|---------|------------------|-------|-------------|
| `qtar <dir\|archive>` | — | both | create or extract a `.tar` (no compression) |
| `gtar <dir\|archive>` | — | both | create or extract a `.tar.gz` (gzip) |
| `bqtar` / `bgtar` | — | both | background, low-priority `qtar` / `gtar` |
| `mu_status` / `mu_ctx` | — | both | compact / detailed environment summary |

> **NOTE:** Add *-h* to any `mu` command for help. A `cp2<Node>` alias is
> `mu cp push` with the node pre-bound; the two forms do the same transfer.
