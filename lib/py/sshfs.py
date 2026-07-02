#!/usr/bin/env python3
"""`mu sshfs` — local-only sshfs mount plane (macOS / fuse-t).

Typer-primary: mounting an HPC dir onto the Mac never runs on a bare HPC, so the
venv is always present; only the final `cd` is shell (`hcd`). Mounting happens
ONLY here (via `hcd`) — `list`/`path`/`add`/`rm` never mount.

Every op that touches a mountpoint is timeout-bounded, so a hung/dead mount
reports a status instead of freezing the terminal.

Registry: a plain file `$MU_SSHFS_ROOT/mounts` (`name  node  remote-path` lines),
managed by `mu sshfs add`/`rm` — never the shell env (a subprocess can't persist
to it) and never config.env.
"""
import os
import shutil
import subprocess
from typing import List

import typer

import hpc

_CTX = {"help_option_names": ["-h", "--help"]}
sshfs_app = typer.Typer(no_args_is_help=True, context_settings=_CTX,
                        help="Mount HPC dirs locally over sshfs (macOS/fuse-t).")

_STAT_TIMEOUT = 4     # s; a slower listing = treat the mount as hung


# --- registry (a file, not the shell env) ------------------------------------
def _root() -> str:
    return os.path.expanduser(os.environ.get("MU_SSHFS_ROOT", "~/hpc_sshfs"))


def _registry_path() -> str:
    return os.path.join(_root(), "registry")


def _mount_dir(name: str) -> str:
    return os.path.join(_root(), "mounts", name)


def _read_registry() -> dict:
    """Return {name: (node, remote_path, read_only)} from the registry file.

    Tab-separated: `name<TAB>node<TAB>path[<TAB>ro]`. Path may contain spaces (not
    tabs); an optional 4th `ro` field marks a read-only mount.
    """
    out = {}
    try:
        with open(_registry_path()) as f:
            for line in f:
                s = line.rstrip("\n")
                if not s.strip() or s.lstrip().startswith("#"):
                    continue
                parts = s.split("\t")
                if len(parts) >= 3:
                    ro = len(parts) >= 4 and parts[3].strip() == "ro"
                    out[parts[0].strip()] = (parts[1].strip(), parts[2].strip(), ro)
    except FileNotFoundError:
        pass
    return out


def _write_registry(entries: dict) -> None:
    path = _registry_path()
    os.makedirs(os.path.dirname(path), exist_ok=True)
    lines = ["# managed by `mu sshfs add` / `mu sshfs rm` — do not hand-edit lightly",
             "# name\tnode\tremote-path\t[ro]"]
    for name in sorted(entries):
        node, rpath, ro = entries[name]
        row = f"{name}\t{node}\t{rpath}"
        if ro:
            row += "\tro"
        lines.append(row)
    with open(path, "w") as f:
        f.write("\n".join(lines) + "\n")


def _complete_mount(incomplete: str) -> List[str]:
    return [n for n in _read_registry() if n.startswith(incomplete)]


# --- mount state (all timeout-bounded — never block on a hung mount) ---------
def _is_mounted(mdir: str) -> bool:
    """True if mdir is an active mountpoint. Parses `mount` — never touches the FS."""
    try:
        out = subprocess.run(["mount"], capture_output=True, text=True, timeout=5).stdout
    except (subprocess.TimeoutExpired, FileNotFoundError):
        return False
    return any(f" on {mdir} (" in ln for ln in out.splitlines())


def _responds(mdir: str) -> bool:
    """True if the mountpoint answers a listing within the timeout (else hung/dead)."""
    try:
        return subprocess.run(["ls", mdir], capture_output=True,
                              timeout=_STAT_TIMEOUT).returncode == 0
    except (subprocess.TimeoutExpired, OSError):
        return False


def _status(name: str) -> str:
    """'mounted' | 'hung' | 'unmounted' — safe even against a hung mount."""
    mdir = _mount_dir(name)
    if not _is_mounted(mdir):
        return "unmounted"
    return "mounted" if _responds(mdir) else "hung"


def _umount(mdir: str) -> bool:
    """Timeout-bounded unmount (umount, then diskutil force). False = couldn't."""
    if not _is_mounted(mdir):
        return True
    for cmd in (["umount", mdir], ["diskutil", "unmount", "force", mdir]):
        try:
            if subprocess.run(cmd, capture_output=True, timeout=10).returncode == 0:
                return True
        except (subprocess.TimeoutExpired, FileNotFoundError):
            continue
    return False


# --- commands ----------------------------------------------------------------
@sshfs_app.command("list")
def list_mounts(verbose: bool = typer.Option(False, "-v", "--verbose", help="also show local mount dirs")):
    """List configured mounts with live status."""
    from rich.console import Console
    from rich.table import Table

    reg = _read_registry()
    if not reg:
        typer.secho("no mounts — add one with `mu sshfs add <name> <node> <path>`", fg="yellow")
        raise typer.Exit(0)

    badge = {"mounted": "[green]● mounted[/]", "hung": "[yellow]⚠ hung[/]",
             "unmounted": "[bright_black]○ not mounted[/]"}
    table = Table(title="[bold]SSHFS Mounts[/]", title_justify="left", header_style="bold")
    table.add_column("Name", style="bold green")
    table.add_column("Node", style="magenta")
    table.add_column("Remote path", style="cyan")
    table.add_column("Access")
    table.add_column("Status")
    if verbose:
        table.add_column("Local", style="bright_black")
    for name in sorted(reg):
        node, rpath, ro = reg[name]
        access = "[yellow]🔒 ro[/]" if ro else "[bright_black]rw[/]"
        row = [name, node, rpath, access, badge.get(_status(name), "?")]
        if verbose:
            row.append(_mount_dir(name))
        table.add_row(*row)
    Console().print(table)


@sshfs_app.command()
def mount(
    name: str = typer.Argument(..., autocompletion=_complete_mount, help="mount name"),
    verbose: bool = typer.Option(False, "-v", "--verbose", help="show the remote target + verbose ssh output"),
):
    """Mount a configured name (used by `hcd`). Auto-remounts a stale mount.

    sshfs keeps stdin/stderr on the terminal, so host-key / Kerberos prompts are
    answerable and connection errors are visible (not swallowed by a pipe).
    """
    if not shutil.which("sshfs"):
        typer.secho("sshfs not found — install fuse-t + sshfs to use mu sshfs", fg="red", err=True)
        raise typer.Exit(3)
    reg = _read_registry()
    if name not in reg:
        typer.secho(f"unknown mount: {name} (see `mu sshfs list`)", fg="red", err=True)
        raise typer.Exit(2)
    node, rpath, ro = reg[name]
    mdir = _mount_dir(name)

    st = _status(name)
    if st == "mounted":
        return  # already live — idempotent
    if st == "hung":
        typer.secho(f"{name}: stale mount — remounting", fg="yellow")
        if not _umount(mdir):
            typer.secho(f"{name}: could not unmount (hung); may need `diskutil unmount force {mdir}` or a restart",
                        fg="red", err=True)
            raise typer.Exit(1)

    target = hpc.resolve(node)
    os.makedirs(mdir, exist_ok=True)
    hpc.ensure_ticket()
    ssh = os.environ.get("MU_SSH", "ssh")
    ssh_cmd = f"{ssh} -o ServerAliveInterval=15 -o ServerAliveCountMax=3" + (" -v" if verbose else "")
    opts = ["-o", f"ssh_command={ssh_cmd}", "-o", "reconnect", "-o", "defer_permissions"]
    if ro:
        opts += ["-o", "ro"]
    cmd = ["sshfs", *opts, f"{target}:{rpath}", mdir]

    dest = f"{target}:{rpath}" if verbose else node
    typer.secho(f"connecting {name} → {dest}" + ("  (ro)" if ro else ""), fg="cyan")

    # Inherit stdin/stderr so host-key / Kerberos prompts are answerable and errors
    # are visible; sshfs daemonizes once the mount is ready. The spinner runs only
    # in non-verbose mode — with -v the raw ssh output flows to the terminal (and is
    # the fallback if a mount ever stalls on a new-host prompt the spinner would hide).
    try:
        if verbose:
            rc = subprocess.run(cmd).returncode
        else:
            from rich.console import Console
            with Console().status(f"[cyan]mounting {name}…", spinner="dots"):
                rc = subprocess.run(cmd).returncode
    except KeyboardInterrupt:
        typer.secho(f"{name}: interrupted", fg="yellow", err=True)
        raise typer.Exit(130)
    if rc == 0 and _is_mounted(mdir):
        typer.secho(f"● mounted {name} → {mdir}", fg="green")
        return
    typer.secho(f"mount failed (sshfs exited {rc}) — retry with `mu sshfs mount {name} -v` for detail",
                fg="red", err=True)
    raise typer.Exit(1)


@sshfs_app.command("umount")
def umount_cmd(name: str = typer.Argument(..., autocompletion=_complete_mount, help="mount name")):
    """Unmount a mount."""
    mdir = _mount_dir(name)
    if not _is_mounted(mdir):
        typer.echo(f"{name}: not mounted")
        return
    if _umount(mdir):
        typer.secho(f"● unmounted {name}", fg="green")
    else:
        typer.secho(f"{name}: could not unmount (hung?); try `diskutil unmount force {mdir}` or a restart",
                    fg="red", err=True)
        raise typer.Exit(1)


@sshfs_app.command("path")
def path_cmd(name: str = typer.Argument(..., autocompletion=_complete_mount, help="mount name")):
    """Print the local mount dir (used by `hcd` to cd). stdout = just the path."""
    if name not in _read_registry():
        typer.secho(f"unknown mount: {name}", fg="red", err=True)
        raise typer.Exit(2)
    typer.echo(_mount_dir(name))


@sshfs_app.command()
def add(
    name: str = typer.Argument(..., help="short handle (used by hcd)"),
    node: str = typer.Argument(..., autocompletion=hpc.complete_node, help="HPC node (from MU_CLUSTERS) or user@host"),
    path: str = typer.Argument(..., help="remote directory to mount"),
    read_only: bool = typer.Option(False, "--ro", "--read-only", help="mount read-only (data to browse, no writes)"),
):
    """Register a new mount (name -> node:path). Does not mount."""
    reg = _read_registry()
    if name in reg:
        typer.secho(f"mount '{name}' already exists → {reg[name][0]}:{reg[name][1]}", fg="yellow", err=True)
        raise typer.Exit(1)
    hpc.resolve(node)  # validate the node resolves (exits 2 if unknown)
    reg[name] = (node, path, read_only)
    _write_registry(reg)
    typer.secho(f"● added {name} → {node}:{path}{'  (ro)' if read_only else ''}", fg="green")


@sshfs_app.command("rm")
def rm(name: str = typer.Argument(..., autocompletion=_complete_mount, help="mount name")):
    """Remove a mount from the registry (does not unmount)."""
    reg = _read_registry()
    if name not in reg:
        typer.secho(f"unknown mount: {name}", fg="red", err=True)
        raise typer.Exit(2)
    if _is_mounted(_mount_dir(name)):
        typer.secho(f"note: {name} is still mounted — `mu sshfs umount {name}` first", fg="yellow")
    del reg[name]
    _write_registry(reg)
    typer.secho(f"● removed {name}", fg="green")
