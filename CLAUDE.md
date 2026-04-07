# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build

```sh
go build -o tmux-nav .
```

After any code change, rebuild and relaunch the navigator for changes to take effect. There are no tests.

## Versioning and releases

This project uses [Semantic Versioning](https://semver.org/) (MAJOR.MINOR.PATCH). The current version is defined as the `Version` constant in `main.go`.

### When releasing a new version

1. Update the `Version` constant in `main.go`.
2. Move the `[Unreleased]` section in `CHANGELOG.md` under a new version heading with the date. Add a fresh `[Unreleased]` section above it. Update the comparison links at the bottom.
3. Commit: `git commit -m "Release vX.Y.Z"`
4. Tag: `git tag vX.Y.Z`
5. Push: `git push origin main --tags`

### Version bumping rules

- **PATCH** (0.1.x) — bug fixes, cosmetic tweaks, no behavior change.
- **MINOR** (0.x.0) — new features, new view modes, new config options. Backward-compatible.
- **MAJOR** (x.0.0) — breaking changes to config options, keybindings, or tmux option names.

### Changelog discipline

All user-visible changes go in `CHANGELOG.md` under `[Unreleased]` using these categories: Added, Changed, Deprecated, Removed, Fixed, Security. Keep entries concise (one line each).

## Architecture

The entire application is a single file: `main.go`. It uses the [Bubbletea](https://github.com/charmbracelet/bubbletea) TUI framework (Elm-style: Model / Update / View).

### Data flow

1. **`fetchAllPanes()`** — queries `tmux list-panes -a` and parses pane ID, PID, command, dir, title, and `@tap_state` (agent state set by tmux-tap). Calls `getGitInfo()` per pane dir.
2. **`getGitInfo()`** — shells out to `git` to get branch, dirty status, root, and main worktree root. Results are cached in `gitCache`.
3. **`groupByRepo()`** — groups panes by `Git.MainRoot`. Panes without git info go under key `"__no_repo__"` (displayed as "No project"), sorted to the top.
4. **`buildItems()`** — flattens `[]RepoGroup` into `[]Item` for the list view (alternating `KindRepo` headers and `KindPane` entries).
5. The `Model` holds both `items` (for list view) and raw `panes` (for kanban views, which re-call `groupByRepo` on render).

### View modes

- **`viewList`** — default; flat scrollable list grouped by repo.
- **`viewKanban`** — horizontal columns, one per repo group.
- **`viewKanbanDrill`** — drill into one repo: three columns (Waiting / Running / Done) based on `AgentState`.

Agent state (`AgentState` int) is read from the tmux pane option `@tap_state`, written by [tmux-tap](https://github.com/purplecones/tmux-tap). The states are: `StateNone`, `StateRunning`, `StateThinking`, `StateDone`, `StateAsking`, `StatePlanReady`.

### Key structs

- `Pane` — a tmux pane with git and agent info
- `RepoGroup` — a named group of panes sharing the same main worktree root
- `Item` — a flat list entry (either a repo header or a pane row)
- `Model` — full app state including cursor positions for all three view modes

### Repo picker (`statePickRepo`)

`findRepos()` walks `~/` (depth 2) and known subdirs like `~/Projects`, `~/code`, etc. (depth 4) to find `.git` dirs. Filtered in real-time via a `textinput` component.
