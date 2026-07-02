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


def _build_rsync_args(src, dst, dry_run, exclude, delete, bwlimit) -> List[str]:
    ssh = os.environ.get("MU_SSH", "ssh")
    args = ["-au", "--partial", "-e", ssh]
    if dry_run:
        args.append("--dry-run")
    if delete:
        args.append("--delete")
    if bwlimit:
        args += ["--bwlimit", bwlimit]
    for ex in exclude or []:
        args += ["--exclude", ex]
    args += [src, dst]
    return args


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
):
    """Copy a local path TO a node (rsync push), with a live progress bar."""
    target = hpc.resolve(node)
    hpc.ensure_ticket()
    args = _build_rsync_args(src, f"{target}:{dst}", dry_run, exclude, delete, bwlimit)
    raise typer.Exit(_rsync_progress(args, f"push {node}"))


@cp_app.command(epilog=_NODES_EPILOG)
def pull(
    node: str = typer.Argument(..., autocompletion=hpc.complete_node, help="node name or user@host"),
    src: str = typer.Argument(..., help="remote source path"),
    dst: str = typer.Argument(..., help="local destination path"),
    dry_run: bool = typer.Option(False, "--dry-run", "-n", help="show what would transfer"),
    exclude: Optional[List[str]] = typer.Option(None, "--exclude", help="rsync exclude pattern (repeatable)"),
    delete: bool = typer.Option(False, "--delete", help="delete extraneous files locally"),
    bwlimit: Optional[str] = typer.Option(None, "--bwlimit", help="rsync bandwidth limit, e.g. 10m"),
):
    """Copy a path FROM a node TO local (rsync pull), with a live progress bar."""
    target = hpc.resolve(node)
    hpc.ensure_ticket()
    args = _build_rsync_args(f"{target}:{src}", dst, dry_run, exclude, delete, bwlimit)
    raise typer.Exit(_rsync_progress(args, f"pull {node}"))


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
