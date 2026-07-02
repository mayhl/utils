#!/usr/bin/env python3
"""mu_log — Python side of the toolkit's logging, mirroring lib/log.sh.

One event, tiered rendering (two independent capability gates):
  * glyph  — a colored symbol when the terminal is UTF-8 (→ ✓ ! ✗), else the
             ASCII `[LEVEL]` label. Gate: stdout encoding is UTF-8, unless MU_ASCII.
  * color  — ANSI color when the target stream is a TTY (honors NO_COLOR / TERM=dumb).
Level meaning always rides on the glyph OR the label — never color alone.

The framework log always gets a plain, timestamped `[ts] [LEVEL] message` line,
regardless of terminal — one uniform audit trail shared with the shell mu_log.
"""
import datetime
import os
import sys

_LOGFILE = os.path.expanduser("~/.cache/mayhl_utils/framework.log")

# level -> (glyph, ansi-color-code, padded-label)
_LEVELS = {
    "INFO":  ("→", "36", "INFO "),   # →  cyan
    "OK":    ("✓", "32", "OK   "),   # ✓  green
    "WARN":  ("!",      "33", "WARN "),   # !  yellow
    "ERROR": ("✗", "31", "ERROR"),   # ✗  red
}


def _use_color(stream) -> bool:
    return (
        hasattr(stream, "isatty") and stream.isatty()
        and os.environ.get("NO_COLOR") is None
        and os.environ.get("TERM", "") != "dumb"
    )


def _use_glyphs(stream) -> bool:
    if os.environ.get("MU_ASCII"):
        return False
    enc = (getattr(stream, "encoding", "") or "").lower()
    return "utf" in enc


def mu_log(level: str, msg: str) -> None:
    """Log to the framework file (always) and render to the terminal (tiered)."""
    level = level.upper()
    glyph, color, label = _LEVELS.get(level, ("·", "", level.ljust(5)))

    # file: always plain + timestamped (shared with lib/log.sh)
    try:
        os.makedirs(os.path.dirname(_LOGFILE), exist_ok=True)
        with open(_LOGFILE, "a") as f:
            ts = datetime.datetime.now().strftime("%Y-%m-%dT%H:%M:%S")
            f.write(f"[{ts}] [{label}] {msg}\n")
    except OSError:
        pass

    # terminal: INFO/OK -> stdout, WARN/ERROR -> stderr (matches lib/log.sh)
    stream = sys.stdout if level in ("INFO", "OK") else sys.stderr
    marker = glyph if _use_glyphs(stream) else f"[{label}]"
    if _use_color(stream) and color:
        marker = f"\033[{color}m{marker}\033[0m"
    print(f"{marker} {msg}", file=stream)
