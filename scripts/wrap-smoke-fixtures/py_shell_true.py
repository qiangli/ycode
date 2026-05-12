#!/usr/bin/env python3
"""Fixture stand-in for a shell=True heavy Python agent (Aider style).

Each subprocess.run uses shell=True so the call goes through /bin/sh -c.
The bash shim ought to intercept the sh invocation; commands inside it
(git, ls, rg) ought to hit the shim too because PATH is inherited.

This fixture is the baseline test for what Aider/Codex-py shell-string
behavior produces without the Piece D runtime hooks in place.
"""
import subprocess
import sys


def main() -> int:
    cmds = [
        "git --version",
        "ls -la",
        "echo 'shell=True ran' | wc -c",
    ]
    rc = 0
    for cmd in cmds:
        # Inherit stderr so the shim's per-exec span debug line reaches
        # ycode wrap's stderr (where the smoke matrix counts it). Real
        # agents capture stderr; Piece D's runtime hook is what closes
        # that observability gap end-to-end.
        r = subprocess.run(cmd, shell=True, stdout=subprocess.PIPE, text=True)
        sys.stdout.write(f"[fixture] $ {cmd}\n  rc={r.returncode}\n")
        if r.returncode != 0:
            rc = r.returncode
    return rc


if __name__ == "__main__":
    sys.exit(main())
