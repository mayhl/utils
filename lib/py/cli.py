#!/usr/bin/env python3
"""mayhl_utils CLI (`mu`).

The management + transfer plane over the shell toolkit. Stateless commands only
(a subprocess can't mutate your shell) — interactive things like ssh logins,
`swap`, and venv activation stay shell.

Config comes from the inherited shell environment (config.env is `export`ed), so
`mu` and the shell codegen read the same single source of truth.

Run via the `mu` shell function (`mu_py lib/py/cli.py …`). Heavy imports (rich)
are deferred into the commands so tab-completion stays fast.
"""

import os
import re
import subprocess
from typing import List, Optional

import typer

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


# --- config, read from the inherited (exported) shell env --------------------
def _cluster_defs():
    """Yield (cluster, domain, [nodes]) from the inherited env config."""
    for c in os.environ.get("MU_CLUSTERS", "").split():
        cu = c.upper()
        domain = os.environ.get(f"MU_CLUSTER_{cu}_DOMAIN", "")
        if not domain:
            continue
        yield c, domain, os.environ.get(f"MU_CLUSTER_{cu}_NODES", "").split()


def node_targets() -> dict:
    """Map every node name -> user@node.domain."""
    user = os.environ.get("MU_HPC_UNAME", "")
    return {
        node: f"{user}@{node}.{domain}"
        for _, domain, nodes in _cluster_defs()
        for node in nodes
    }


def _complete_node(incomplete: str) -> List[str]:
    return [n for n in node_targets() if n.startswith(incomplete)]


def _nodes_epilog() -> str:
    """Live node list for --help, read from the inherited env at import time."""
    nodes = sorted(node_targets())
    if not nodes:
        return "No nodes configured (is MU_CLUSTERS set?)."
    return "Known nodes: " + ", ".join(nodes) + "  ·  see 'mu cp nodes' for targets."


_NODES_EPILOG = _nodes_epilog()


def _resolve(node_or_target: str) -> str:
    """Accept a bare node name (`mike`) or an explicit `user@host` target."""
    if "@" in node_or_target:
        return node_or_target
    targets = node_targets()
    if node_or_target not in targets:
        known = ", ".join(sorted(targets)) or "(none — is MU_CLUSTERS set?)"
        typer.secho(f"unknown node: {node_or_target}", fg="red", err=True)
        typer.secho(f"known nodes: {known}", err=True)
        raise typer.Exit(2)
    return targets[node_or_target]


# --- auth: in-body so --help / completion never trigger pkinit ---------------
def _ensure_ticket() -> None:
    user = os.environ.get("MU_HPC_UNAME", "")
    if not user:
        return
    try:
        klist = subprocess.run(["klist"], capture_output=True, text=True)
    except FileNotFoundError:
        return  # no kerberos here (e.g. hpc login node) — nothing to do
    if user in klist.stdout:
        return
    typer.secho(f"No Kerberos ticket for {user}; running pkinit...", fg="cyan")
    subprocess.run(["pkinit", user])


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
    node: str = typer.Argument(..., autocompletion=_complete_node, help="node name or user@host"),
    src: str = typer.Argument(..., help="local source path"),
    dst: str = typer.Argument(..., help="remote destination path"),
    dry_run: bool = typer.Option(False, "--dry-run", "-n", help="show what would transfer"),
    exclude: Optional[List[str]] = typer.Option(None, "--exclude", help="rsync exclude pattern (repeatable)"),
    delete: bool = typer.Option(False, "--delete", help="delete extraneous files on the remote"),
    bwlimit: Optional[str] = typer.Option(None, "--bwlimit", help="rsync bandwidth limit, e.g. 10m"),
):
    """Copy a local path TO a node (rsync push), with a live progress bar."""
    target = _resolve(node)
    _ensure_ticket()
    args = _build_rsync_args(src, f"{target}:{dst}", dry_run, exclude, delete, bwlimit)
    raise typer.Exit(_rsync_progress(args, f"push {node}"))


@cp_app.command(epilog=_NODES_EPILOG)
def pull(
    node: str = typer.Argument(..., autocompletion=_complete_node, help="node name or user@host"),
    src: str = typer.Argument(..., help="remote source path"),
    dst: str = typer.Argument(..., help="local destination path"),
    dry_run: bool = typer.Option(False, "--dry-run", "-n", help="show what would transfer"),
    exclude: Optional[List[str]] = typer.Option(None, "--exclude", help="rsync exclude pattern (repeatable)"),
    delete: bool = typer.Option(False, "--delete", help="delete extraneous files locally"),
    bwlimit: Optional[str] = typer.Option(None, "--bwlimit", help="rsync bandwidth limit, e.g. 10m"),
):
    """Copy a path FROM a node TO local (rsync pull), with a live progress bar."""
    target = _resolve(node)
    _ensure_ticket()
    args = _build_rsync_args(f"{target}:{src}", dst, dry_run, exclude, delete, bwlimit)
    raise typer.Exit(_rsync_progress(args, f"pull {node}"))


@cp_app.command("nodes")
def cp_nodes():
    """List the known HPC nodes (from MU_CLUSTERS)."""
    from rich.console import Console
    from rich.panel import Panel
    from rich.table import Table

    defs = list(_cluster_defs())
    if not defs:
        typer.secho("no nodes — is MU_CLUSTERS set?", fg="yellow")
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
