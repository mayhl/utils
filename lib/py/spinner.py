#!/usr/bin/env python3
"""Rich spinner for mayhl_utils: run a command under a live spinner.

Usage:
    spinner.py <message> <command> [args...]

Shows an animated spinner with <message> while <command> runs, then a ✓/✗
summary line. Exits with the command's return code. Run via `mu_py`.
"""
import subprocess
import sys

from rich.console import Console


def main(argv):
    if len(argv) < 3:
        print("usage: spinner.py <message> <command> [args...]", file=sys.stderr)
        return 2

    message, command = argv[1], argv[2:]
    console = Console()

    with console.status(f"[cyan]{message}[/]", spinner="dots"):
        proc = subprocess.run(command)

    if proc.returncode == 0:
        console.print(f"[green]✓[/] {message}")
    else:
        console.print(f"[red]✗[/] {message} (exit {proc.returncode})")
    return proc.returncode


if __name__ == "__main__":
    sys.exit(main(sys.argv))
