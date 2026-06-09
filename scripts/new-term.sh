#!/usr/bin/env bash
# scripts/new-term.sh — open a new terminal window running a given command.
#
# Cross-platform helper that detects the host OS and dispatches to the
# native terminal emulator:
#
#   - macOS:   iTerm if running, otherwise Terminal.app (AppleScript via
#              osascript). Opens a new TAB in the front window; falls back
#              to a new window when the app has no windows open yet.
#              Terminal.app does this by sending Cmd+T to System Events,
#              so the script needs Accessibility permission the first run.
#   - Linux:   first available of gnome-terminal / konsole / xfce4-terminal /
#              alacritty / kitty / wezterm / tilix / terminator / xterm
#   - Windows: Windows Terminal (wt.exe) if present, otherwise cmd.exe
#              (works from Git Bash, MSYS, Cygwin, or WSL with Windows interop)
#
# The new window closes when the command exits. Set NEW_TERM_KEEP_OPEN=1
# to drop into an interactive bash instead so the window stays open.
#
# The command runs in $TMPDIR (or /tmp if unset). Override with
# NEW_TERM_CWD=/some/path.
#
# Usage:
#   scripts/new-term.sh "ycode wrap -- codex"
#   scripts/new-term.sh ycode wrap -- codex
#
# Quoting:
#   All arguments are concatenated with single spaces and run as one shell
#   command. Quote anything tricky once at the caller; the script does not
#   re-quote per token.

set -u

if [[ $# -lt 1 ]]; then
  echo "usage: $(basename "$0") <command> [args...]" >&2
  exit 2
fi

CMD="$*"
KEEP_OPEN="${NEW_TERM_KEEP_OPEN:-0}"
CWD="${NEW_TERM_CWD:-${TMPDIR:-/tmp}}"

# Strip trailing slash from $TMPDIR (macOS sets it with one) so the cd path
# reads cleanly in any user-visible echoes.
CWD="${CWD%/}"

printf -v CWD_Q '%q' "$CWD"
PREFIX="cd $CWD_Q && "
if [[ "$KEEP_OPEN" == "1" ]]; then
  SUFFIX="; exec bash"
else
  SUFFIX=""
fi
CMD="${PREFIX}${CMD}"

case "$(uname -s)" in
  Darwin)
    # Escape backslashes and double-quotes for the AppleScript string literal.
    esc=${CMD//\\/\\\\}
    esc=${esc//\"/\\\"}
    if pgrep -xq iTerm2 2>/dev/null || pgrep -xq iTerm 2>/dev/null; then
      # iTerm2 — new tab in current window, or new window if none exists.
      osascript <<EOF
tell application "iTerm"
  activate
  if (count of windows) = 0 then
    create window with default profile
  else
    tell current window to create tab with default profile
  end if
  tell current session of current window to write text "$esc$SUFFIX"
end tell
EOF
    else
      # Terminal.app — new tab via Cmd+T keystroke (no first-class tab API).
      # Falls back to a new window when Terminal has no window yet OR when
      # Accessibility permission for keystrokes hasn't been granted.
      osascript <<EOF
tell application "Terminal"
  activate
  if (count of windows) = 0 then
    do script "$esc$SUFFIX"
  else
    try
      tell application "System Events" to keystroke "t" using command down
      delay 0.2
      do script "$esc$SUFFIX" in front window
    on error errMsg number errNum
      -- Accessibility denied (-1719/-1002) or any other keystroke failure:
      -- silently fall back to a new window so the command still runs.
      do script "$esc$SUFFIX"
    end try
  end if
end tell
EOF
    fi
    ;;

  Linux)
    for term in x-terminal-emulator gnome-terminal konsole xfce4-terminal \
                alacritty kitty wezterm tilix terminator xterm; do
      if command -v "$term" >/dev/null 2>&1; then
        case "$term" in
          gnome-terminal)
            "$term" -- bash -lc "$CMD$SUFFIX" &
            ;;
          wezterm)
            "$term" start -- bash -lc "$CMD$SUFFIX" &
            ;;
          *)
            "$term" -e bash -lc "$CMD$SUFFIX" &
            ;;
        esac
        exit 0
      fi
    done
    echo "new-term: no supported terminal emulator found on PATH" >&2
    exit 1
    ;;

  MINGW*|MSYS*|CYGWIN*)
    if command -v wt.exe >/dev/null 2>&1; then
      wt.exe new-tab bash -lc "$CMD$SUFFIX" &
    else
      start cmd //k "bash -lc \"$CMD$SUFFIX\""
    fi
    ;;

  Linux*Microsoft*|*microsoft*)
    # WSL — covered by Linux uname; fall through there.
    ;;

  *)
    # WSL detection (uname -s prints Linux on WSL too, so this is a backstop
    # for unusual environments).
    if grep -qiE 'microsoft|wsl' /proc/version 2>/dev/null; then
      if command -v wt.exe >/dev/null 2>&1; then
        wt.exe new-tab bash -lc "$CMD$SUFFIX" &
        exit 0
      fi
    fi
    echo "new-term: unsupported platform $(uname -s)" >&2
    exit 1
    ;;
esac
