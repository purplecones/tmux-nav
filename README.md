# tmux-nav

A tmux popup navigator for agentic coding sessions. Groups panes by git repository and displays live agent state (running, waiting, plan ready, etc.) sourced from [tmux-tap](https://github.com/mirzajoldic/tmux-tap).

## Requirements

- tmux ≥ 3.0
- Go ≥ 1.21 (for the build step on install)
- [tmux-tap](https://github.com/mirzajoldic/tmux-tap) — must be listed before tmux-nav

## Installation

### Via TPM (recommended)

```tmux
set -g @plugin 'mirzajoldic/tmux-tap'
set -g @plugin 'mirzajoldic/tmux-nav'
```

Then `prefix + I` to install. The Go binary is built automatically on first load.

### Manual

```sh
git clone https://github.com/mirzajoldic/tmux-nav ~/.tmux/plugins/tmux-nav
cd ~/.tmux/plugins/tmux-nav && go build -o tmux-nav .
```

Add to `~/.tmux.conf`:

```tmux
run '~/.tmux/plugins/tmux-nav/tmux-nav.tmux'
```

## Configuration

```tmux
# Key to open the popup (default: s → prefix + s)
set -g @tmux_nav_key "s"

# Popup dimensions
set -g @tmux_nav_width  "80%"
set -g @tmux_nav_height "80%"
```

## Keybindings

### List view (default)

| Key | Action |
|-----|--------|
| `j` / `k` | Navigate panes |
| `enter` | Switch to pane |
| `b` | Switch to kanban view |
| `x` | Kill pane |
| `w` | New window in current dir |
| `n` | New session (repo picker) |
| `t` | New git worktree |
| `q` | Quit |

### Kanban view

| Key | Action |
|-----|--------|
| `h` / `l` | Move between repo columns |
| `j` / `k` | Move between cards |
| `tab` | Drill into repo pipeline |
| `enter` | Switch to pane |
| `b` | Back to list view |
| `q` | Quit |

### Pipeline drill-down

| Key | Action |
|-----|--------|
| `h` / `l` | Move between Waiting / Running / Done columns |
| `j` / `k` | Move between cards |
| `enter` | Switch to pane |
| `esc` | Back to kanban |
| `q` | Quit |

## Agent states

States are provided by tmux-tap and displayed as badges:

| Badge | Meaning |
|-------|---------|
| `● running` | Agent is actively working |
| `📋 plan ready` | Agent finished planning, awaiting approval |
| `? input` | Agent is blocked, waiting for a new prompt |
| `? choose` | Agent asked a question or presented options |
| `✓ done` | Agent completed its task |
