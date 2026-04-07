# Changelog

All notable changes to this project will be documented in this file.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.3.0] - 2026-04-06

### Added
- Duration display next to agent state badge (e.g. "3m12s") via tmux-tap's `@tap_state_since`

## [0.2.0] - 2026-04-06

### Added
- Unseen indicator (bright green `●`) for done panes not yet viewed by the user
- Indicator clears when the user switches to the pane or it becomes the active tmux pane

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

[Unreleased]: https://github.com/purplecones/tmux-nav/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/purplecones/tmux-nav/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/purplecones/tmux-nav/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/purplecones/tmux-nav/releases/tag/v0.1.0
