#!/usr/bin/env bash
# tmux-nav — TPM entry point
# Builds the Go binary if needed and registers the popup keybinding.

PLUGIN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY="$PLUGIN_DIR/tmux-nav"

# ── Dependency check ──────────────────────────────────────────────────────────
if [[ ! -d "${HOME}/.tmux-tap" ]]; then
  echo "[tmux-nav] WARNING: tmux-tap does not appear to be installed." >&2
  echo "[tmux-nav] Add 'set -g @plugin purplecones/tmux-tap' BEFORE tmux-nav in tmux.conf" >&2
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

# ── Pane border titles with agent-state coloring ─────────────────────────────
# Colors pane borders based on @tap_state set by tmux-tap:
#   running → orange, thinking → yellow, done → green,
#   asking → purple, plan_ready → blue
tmux set-option -g pane-border-status top
tmux set-option -g pane-border-format "#{?#{==:#{@tap_state},running},#[bg=colour208 fg=black],#{?#{==:#{@tap_state},thinking},#[bg=colour214 fg=black],#{?#{==:#{@tap_state},done},#[bg=colour71 fg=black],#{?#{==:#{@tap_state},asking},#[bg=colour135 fg=white],#{?#{==:#{@tap_state},plan_ready},#[bg=colour33 fg=white],}}}}} #{?#{pane_title},#{pane_title},#{b:pane_current_path}} #[default]"
tmux set-option -g pane-border-style fg=grey
tmux set-option -g pane-active-border-style fg=green

# ── Register keybinding ───────────────────────────────────────────────────────
KEY=$(tmux show-options -gqv "@tmux_nav_key" 2>/dev/null)
KEY="${KEY:-s}"
WIDTH=$(tmux show-options -gqv "@tmux_nav_width" 2>/dev/null)
WIDTH="${WIDTH:-80%}"
HEIGHT=$(tmux show-options -gqv "@tmux_nav_height" 2>/dev/null)
HEIGHT="${HEIGHT:-80%}"

tmux bind-key "$KEY" display-popup -E -w "$WIDTH" -h "$HEIGHT" "$BINARY"
