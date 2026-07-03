#!/usr/bin/env python3
"""mayhl_utils CLI (`mu`).

The management + transfer plane over the shell toolkit. Stateless commands only
(a subprocess can't mutate your shell) — interactive things like ssh logins,
`swap`, and venv activation stay shell.

Config comes from the inherited shell environment (config.env is `export`ed), so
`mu` and the shell codegen read the same single source of truth.

Run via the `mu` shell function (`mu_py lib/py/cli.py …`). Heavy imports (rich)
are deferred into the commands so tab-completion stays fast. Shared cluster/auth
helpers live in hpc.py; the sshfs plane in sshfs.py.
"""
import os
import re
import shlex
import subprocess
from typing import List, Optional

import typer

import hpc
from log import mu_log
from sshfs import sshfs_app

# -h as well as --help, everywhere.
_CTX = {"help_option_names": ["-h", "--help"]}

app = typer.Typer(
    add_completion=True,
    no_args_is_help=True,
    context_settings=_CTX,
    help="mayhl_utils CLI — management + transfers over the HPC toolkit.",
)
cp_app = typer.Typer(no_args_is_help=True, context_settings=_CTX, help="Copy files to/from HPC nodes (rsync).")
app.add_typer(cp_app, name="cp")
app.add_typer(sshfs_app, name="sshfs")

_NODES_EPILOG = hpc.nodes_epilog()


# --- rsync + rich progress ---------------------------------------------------
# rsync --info=progress2 line, e.g. "  1,234,567  45%  12.34MB/s    0:00:12"
_PROGRESS = re.compile(r"([\d,]+)\s+(\d+)%\s+(\S+/s)\s+(\d+:\d\d:\d\d)")


def _split_stream(stream):
    """Yield tokens split on \\r or \\n (rsync updates progress with \\r)."""
    buf = ""
    while True:
        chunk = stream.read(64)
        if not chunk:
            break
        buf += chunk
        parts = re.split(r"[\r\n]", buf)
        buf = parts.pop()
        for p in parts:
            yield p
    if buf:
        yield buf


_BASE_DEFAULT = "-au --partial"
_PARTIAL_DIR = ".rsync-partial"

# Progress/output flags the tool owns; stripped from user-supplied base/ropt so
# they can't corrupt the --info=progress2 parser. Short clusters keep their other
# letters (-avuP -> -au). Long forms drop whole.
_PROGRESS_LONG = {"--verbose", "--progress"}
# raw-option -> canonical key, for cross-layer duplicate detection. --exclude is
# intentionally absent: multiple excludes stack, they don't conflict.
_SHORT_KEY = {"z": "--compress", "c": "--checksum"}
_LONG_KEY = {
    "--compress": "--compress", "--checksum": "--checksum",
    "--timeout": "--timeout", "--partial-dir": "--partial-dir",
    "--delete": "--delete", "--bwlimit": "--bwlimit",
}


def _crack_progress(tokens):
    """Split progress/verbose flags out of a token list -> (kept, stripped).

    Long flags (--verbose/--progress/--info*) drop whole; short clusters keep
    their non-progress letters (e.g. -avuP -> -au, stripping v and P).
    """
    kept, stripped = [], []
    for tok in tokens:
        if tok in _PROGRESS_LONG or tok.startswith("--info"):
            stripped.append(tok)
        elif len(tok) > 1 and tok[0] == "-" and tok[1] != "-":
            bad = [c for c in tok[1:] if c in "vP"]
            if bad:
                stripped += [f"-{c}" for c in bad]
                clean = "".join(c for c in tok[1:] if c not in "vP")
                if clean:
                    kept.append(f"-{clean}")
            else:
                kept.append(tok)
        else:
            kept.append(tok)
    return kept, stripped


def _sanitize(tokens, source):
    """Drop tool-owned progress flags from a user layer, warning once."""
    kept, stripped = _crack_progress(tokens)
    if stripped:
        uniq = " ".join(dict.fromkeys(stripped))
        mu_log("WARN", f"ignoring progress flags in {source} ({uniq}); the tool "
                       "owns progress (default aggregate bar; -v for per-file)")
    return kept


def _canon_keys(tokens):
    """Canonical option keys present in a raw token list (for dup detection)."""
    keys = set()
    for tok in tokens:
        head = tok.split("=", 1)[0]
        if head in _LONG_KEY:
            keys.add(_LONG_KEY[head])
        elif len(tok) > 1 and tok[0] == "-" and tok[1] != "-":
            keys |= {_SHORT_KEY[c] for c in tok[1:] if c in _SHORT_KEY}
    return keys


def _build_rsync_args(src, dst, *, dry_run, exclude, delete, bwlimit,
                      compress, checksum, timeout, partial_dir, ropt) -> List[str]:
    ssh = os.environ.get("MU_SSH", "ssh")
    # Quiet the transfer transport (default -q) so the login banner is dropped;
    # interactive logins go through mu_ssh_login and keep it.
    transport = f"{ssh} {os.environ.get('MU_SSH_TRANSFER_OPTS', '-q')}".strip()

    # Layer order (later wins): env base -> named flags -> --ropt -> progress
    # (progress is prepended by the runner). Each user layer is progress-sanitized.
    env = _sanitize(shlex.split(os.environ.get("MU_HPC_RSYNC_OPTS", _BASE_DEFAULT)),
                    "MU_HPC_RSYNC_OPTS")

    # --ropt: progress-sanitize, then drop exact repeats.
    ropt_clean, seen = [], set()
    for o in _sanitize(list(ropt or []), "--ropt"):
        if o in seen:
            mu_log("WARN", f"duplicate --ropt '{o}' ignored")
            continue
        seen.add(o)
        ropt_clean.append(o)

    # Warn when a named flag repeats an option already set in a raw layer.
    raw_keys = _canon_keys(env) | _canon_keys(ropt_clean)
    named: List[str] = []

    def _flag(active, tokens, key):
        if not active:
            return
        if key in raw_keys:
            mu_log("WARN", f"{key} set via both a flag and a raw opt; "
                           "rightmost wins (--ropt > flag > env)")
        named.extend(tokens)

    _flag(compress, ["-z"], "--compress")
    _flag(checksum, ["-c"], "--checksum")
    _flag(delete, ["--delete"], "--delete")
    _flag(bool(bwlimit), ["--bwlimit", bwlimit or ""], "--bwlimit")
    _flag(timeout > 0, ["--timeout", str(timeout)], "--timeout")
    _flag(partial_dir, [f"--partial-dir={_PARTIAL_DIR}"], "--partial-dir")
    for ex in exclude or []:
        named += ["--exclude", ex]
    if dry_run:
        named.append("--dry-run")

    return [*env, *named, *ropt_clean, "-e", transport, src, dst]


def _rsync_progress(rsync_args: List[str], label: str) -> int:
    """Run rsync with a rich bar; return rsync's exit code.

    stderr is left attached to the terminal so ssh host-key prompts and errors
    surface instead of being swallowed.
    """
    from rich.progress import BarColumn, Progress, TextColumn

    cmd = ["rsync", "--info=progress2", *rsync_args]
    proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=None, text=True)
    columns = [
        TextColumn("[cyan]{task.description}"),
        BarColumn(),
        TextColumn("{task.percentage:>3.0f}%"),
        TextColumn("{task.fields[rate]:>11}"),
        TextColumn("ETA {task.fields[eta]}"),
    ]
    with Progress(*columns) as progress:
        task = progress.add_task(label, total=100, rate="", eta="--:--:--")
        for line in _split_stream(proc.stdout):
            m = _PROGRESS.search(line)
            if m:
                progress.update(task, completed=int(m.group(2)), rate=m.group(3), eta=m.group(4))
        progress.update(task, completed=100)
    proc.wait()
    return proc.returncode


def _rsync_run(rsync_args: List[str], label: str, verbose: bool) -> int:
    """Aggregate Rich bar by default; -v streams raw per-file rsync (no bar).

    The per-file `-vP` output can't be folded into an aggregate bar, so verbose
    mode bypasses the Rich renderer and attaches rsync straight to the terminal.
    """
    if verbose:
        return subprocess.run(["rsync", "-vP", *rsync_args]).returncode
    return _rsync_progress(rsync_args, label)


# --- commands ----------------------------------------------------------------
@cp_app.command(epilog=_NODES_EPILOG)
def push(
    node: str = typer.Argument(..., autocompletion=hpc.complete_node, help="node name or user@host"),
    src: str = typer.Argument(..., help="local source path"),
    dst: str = typer.Argument(..., help="remote destination path"),
    dry_run: bool = typer.Option(False, "--dry-run", "-n", help="show what would transfer"),
    exclude: Optional[List[str]] = typer.Option(None, "--exclude", help="rsync exclude pattern (repeatable)"),
    delete: bool = typer.Option(False, "--delete", help="delete extraneous files on the remote"),
    bwlimit: Optional[str] = typer.Option(None, "--bwlimit", help="rsync bandwidth limit, e.g. 10m"),
    compress: bool = typer.Option(False, "--compress", "-z", help="compress in transit (skip for pre-compressed data)"),
    checksum: bool = typer.Option(False, "--checksum", "-c", help="verify by checksum, not size+mtime"),
    timeout: int = typer.Option(0, "--timeout", help="I/O timeout in seconds (0 = none)"),
    partial_dir: bool = typer.Option(True, "--partial-dir/--no-partial-dir", help="keep partials in .rsync-partial for cross-run resume"),
    ropt: Optional[List[str]] = typer.Option(None, "--ropt", help="extra raw rsync option (repeatable)"),
    verbose: bool = typer.Option(False, "--verbose", "-v", help="per-file output instead of the aggregate bar"),
):
    """Copy a local path TO a node (rsync push), with a live progress bar."""
    target = hpc.resolve(node)
    hpc.ensure_ticket()
    args = _build_rsync_args(src, f"{target}:{dst}", dry_run=dry_run, exclude=exclude,
                             delete=delete, bwlimit=bwlimit, compress=compress, checksum=checksum,
                             timeout=timeout, partial_dir=partial_dir, ropt=ropt)
    raise typer.Exit(_rsync_run(args, f"push {node}", verbose))


@cp_app.command(epilog=_NODES_EPILOG)
def pull(
    node: str = typer.Argument(..., autocompletion=hpc.complete_node, help="node name or user@host"),
    src: str = typer.Argument(..., help="remote source path"),
    dst: str = typer.Argument(..., help="local destination path"),
    dry_run: bool = typer.Option(False, "--dry-run", "-n", help="show what would transfer"),
    exclude: Optional[List[str]] = typer.Option(None, "--exclude", help="rsync exclude pattern (repeatable)"),
    delete: bool = typer.Option(False, "--delete", help="delete extraneous files locally"),
    bwlimit: Optional[str] = typer.Option(None, "--bwlimit", help="rsync bandwidth limit, e.g. 10m"),
    compress: bool = typer.Option(False, "--compress", "-z", help="compress in transit (skip for pre-compressed data)"),
    checksum: bool = typer.Option(False, "--checksum", "-c", help="verify by checksum, not size+mtime"),
    timeout: int = typer.Option(0, "--timeout", help="I/O timeout in seconds (0 = none)"),
    partial_dir: bool = typer.Option(True, "--partial-dir/--no-partial-dir", help="keep partials in .rsync-partial for cross-run resume"),
    ropt: Optional[List[str]] = typer.Option(None, "--ropt", help="extra raw rsync option (repeatable)"),
    verbose: bool = typer.Option(False, "--verbose", "-v", help="per-file output instead of the aggregate bar"),
):
    """Copy a path FROM a node TO local (rsync pull), with a live progress bar."""
    target = hpc.resolve(node)
    hpc.ensure_ticket()
    args = _build_rsync_args(f"{target}:{src}", dst, dry_run=dry_run, exclude=exclude,
                             delete=delete, bwlimit=bwlimit, compress=compress, checksum=checksum,
                             timeout=timeout, partial_dir=partial_dir, ropt=ropt)
    raise typer.Exit(_rsync_run(args, f"pull {node}", verbose))


@cp_app.command("nodes")
def cp_nodes():
    """List the known HPC nodes (from MU_CLUSTERS)."""
    from rich.console import Console
    from rich.panel import Panel
    from rich.table import Table

    defs = list(hpc.cluster_defs())
    if not defs:
        mu_log("WARN", "no nodes — is MU_CLUSTERS set?")
        raise typer.Exit(1)

    user = os.environ.get("MU_HPC_UNAME", "?")
    table = Table(title="[bold]HPC Nodes[/]", title_justify="left", header_style="bold")
    table.add_column("Cluster", style="magenta")
    table.add_column("Node", style="bold green")
    table.add_column("Host", style="cyan")
    for i, (cluster, domain, nodes) in enumerate(defs):
        for j, node in enumerate(sorted(nodes)):
            table.add_row(cluster if j == 0 else "", node, f"{node}.{domain}")
        if i < len(defs) - 1:
            table.add_section()

    console = Console()
    console.print(Panel.fit(f"[bold]Username:[/]  [bold cyan]{user}[/]", border_style="cyan"))
    console.print()
    console.print(table)


if __name__ == "__main__":
    app(prog_name="mu")
