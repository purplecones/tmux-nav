# Changelog

All notable changes to this project will be documented in this file.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-04-06

### Added
- List view with panes grouped by git repo
- Kanban view with one column per repo group
- Kanban drill view with agent state columns (Waiting / Running / Done)
- Agent state integration via tmux-tap (`@tap_state`)
- Repo picker with fuzzy filtering (`findRepos()`)
- Pane border coloring based on agent state
- Configurable keybinding, popup width, and height via tmux options
- Auto-build of Go binary via TPM entry point

[Unreleased]: https://github.com/purplecones/tmux-nav/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/purplecones/tmux-nav/releases/tag/v0.1.0
