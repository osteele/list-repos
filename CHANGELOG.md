# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed
- Repair no longer runs its pre-checkout safety commit in the wrong directory, which silently skipped preserving local working-tree files (2026-08-16)
- Repair keeps the `.git.broken` backup instead of deleting it, so stashes, reflog, and unpushed branches survive a recovery (2026-08-16)
- A repository that fails to scan is reported as an error rather than as corrupted, so it no longer invites the `.git`-moving repair action (2026-08-16)
- Repositories whose status check fails appear in the table instead of being dropped from it (2026-08-16)
- Ahead/behind counts that cannot be determined report `?` rather than claiming zero (2026-08-16)
- Push detects a missing upstream via `rev-parse` instead of matching English text in `git push` output (2026-08-16)
- Handle non-git jj repos gracefully in status check (2025-11-10)

### Changed
- Batch output is now Name/VCS/Status, listing only noteworthy states, with color on terminals, `tabwriter` alignment, and a summary line (2026-08-16)
- Non-repository directories are hidden unless `--all` is given (2026-08-16)
- With `--filter`, exit 1 when anything matches and 2 for usage errors, so filters work as scriptable checks (2026-08-16)
- All `git`/`jj` subprocesses run through a single helper with timeouts and a non-interactive environment, so a credential prompt fails fast instead of hanging (2026-08-16)
- Per-VCS behavior moved behind a `Backend` interface, selected once from the detected repo type (2026-08-16)

### Added
- TUI: scrolling for lists longer than the window, `enter` for a detail view with full action output, and `?` for the full key list (2026-08-16)
- TUI: confirmation prompts for repair and for replacing an existing `origin` (2026-08-16)
- TUI: an editable commit message, pre-filled with the default (2026-08-16)
- TUI: per-repository busy state, so a second action cannot race the first; failures persist until the next keypress (2026-08-16)
- Improve jujutsu status checking and error reporting (2025-11-04)
- Expression-based filtering and sorting capabilities (2025-09-15)
- Parallel directory status collection for improved performance (2025-09-15)
- CI workflow for automated testing (2025-09-14)

### Documentation
- Introduce expression-based filtering and sorting in documentation (2025-09-15)

## [0.1.0] - 2025-09-14

### Added
- Initial release
- Scan subdirectories for Git and Jujutsu repositories
- Report repository status (dirty, remote, ahead)
- Display results in formatted table
- Support for both Git and Jujutsu version control systems
