#!/usr/bin/env python3
"""Fixture stand-in for a list-arg subprocess.run Python agent.

These calls pass argv as a list, no shell. Lookup happens via execvp ->
PATH, so the wrap shim should catch each binary. This is the "easy"
path; even without Piece D runtime hooks the shim should see every
shell-out.

One call uses an absolute path on purpose — the shim cannot intercept
those without Piece D. The matrix flags this row when the span count is
N-1 instead of N.
"""
import subprocess
import sys


def main() -> int:
    calls = [
        (["git", "--version"], "PATH lookup"),
        (["ls", "-la"], "PATH lookup"),
        (["/usr/bin/env", "echo", "absolute path"], "BYPASSES wrap shim"),
    ]
    rc = 0
    for argv, note in calls:
        # Inherit stderr — see py_shell_true.py for the rationale.
        r = subprocess.run(argv, stdout=subprocess.PIPE, text=True)
        sys.stdout.write(f"[fixture] {note}: {argv}\n  rc={r.returncode}\n")
        if r.returncode != 0:
            rc = r.returncode
    return rc


if __name__ == "__main__":
    sys.exit(main())
