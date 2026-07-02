#!/usr/bin/env python3
"""Shared HPC helpers: cluster/node resolution + Kerberos auth.

Imported by both cli.py (cp) and sshfs.py. Kept in its own module so they can
share without a circular import — cli.py runs as __main__, so `from cli import …`
would not resolve. Config comes from the inherited (exported) shell env.
"""
import os
import subprocess
from typing import List

import typer

from log import mu_log


def cluster_defs():
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
        for _, domain, nodes in cluster_defs()
        for node in nodes
    }


def complete_node(incomplete: str) -> List[str]:
    return [n for n in node_targets() if n.startswith(incomplete)]


def nodes_epilog() -> str:
    """Live node list for --help, read from the inherited env at import time."""
    nodes = sorted(node_targets())
    if not nodes:
        return "No nodes configured (is MU_CLUSTERS set?)."
    return "Known nodes: " + ", ".join(nodes) + "  ·  see 'mu cp nodes' for targets."


def resolve(node_or_target: str) -> str:
    """Accept a bare node name (`mike`) or an explicit `user@host` target."""
    if "@" in node_or_target:
        return node_or_target
    targets = node_targets()
    if node_or_target not in targets:
        known = ", ".join(sorted(targets)) or "(none — is MU_CLUSTERS set?)"
        mu_log("ERROR", f"unknown node: {node_or_target} (known: {known})")
        raise typer.Exit(2)
    return targets[node_or_target]


def ensure_ticket() -> None:
    """Obtain a Kerberos ticket if none is present. In-body so --help/completion
    never trigger pkinit."""
    user = os.environ.get("MU_HPC_UNAME", "")
    if not user:
        return
    try:
        klist = subprocess.run(["klist"], capture_output=True, text=True)
    except FileNotFoundError:
        return  # no kerberos here (e.g. hpc login node) — nothing to do
    if user in klist.stdout:
        return
    mu_log("INFO", f"No Kerberos ticket for {user}; running pkinit…")
    subprocess.run(["pkinit", user])
