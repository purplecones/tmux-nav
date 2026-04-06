#!/usr/bin/env bash
# tmux-nav — TPM entry point
# Builds the Go binary if needed and registers the popup keybinding.

PLUGIN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY="$PLUGIN_DIR/tmux-nav"

# ── Dependency check ──────────────────────────────────────────────────────────
if [[ ! -d "${HOME}/.tmux-tap" ]]; then
  echo "[tmux-nav] WARNING: tmux-tap does not appear to be installed." >&2
  echo "[tmux-nav] Add 'set -g @plugin mirzajoldic/tmux-tap' BEFORE tmux-nav in tmux.conf" >&2
fi

# ── Build binary if missing or source is newer ────────────────────────────────
if [[ ! -x "$BINARY" ]] || [[ "$PLUGIN_DIR/main.go" -nt "$BINARY" ]]; then
  if command -v go &>/dev/null; then
    echo "[tmux-nav] Building binary..."
    (cd "$PLUGIN_DIR" && go build -o tmux-nav .) || {
      echo "[tmux-nav] ERROR: go build failed" >&2
      exit 1
    }
  else
    echo "[tmux-nav] ERROR: Go is required. Install it from https://go.dev/dl/ then run:" >&2
    echo "[tmux-nav]   cd $PLUGIN_DIR && go build -o tmux-nav ." >&2
    exit 1
  fi
fi

# ── Register keybinding ───────────────────────────────────────────────────────
KEY=$(tmux show-options -gqv "@tmux_nav_key" 2>/dev/null)
KEY="${KEY:-s}"
WIDTH=$(tmux show-options -gqv "@tmux_nav_width" 2>/dev/null)
WIDTH="${WIDTH:-80%}"
HEIGHT=$(tmux show-options -gqv "@tmux_nav_height" 2>/dev/null)
HEIGHT="${HEIGHT:-80%}"

tmux bind-key "$KEY" display-popup -E -w "$WIDTH" -h "$HEIGHT" "$BINARY"
