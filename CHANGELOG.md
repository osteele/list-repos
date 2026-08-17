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
- TUI: the nesting indent leads the whole row, so a child's status and type glyphs are indented under its parent rather than only its name; the detail column stays aligned across depths (2026-08-16)
- TUI: a repository with no remote shows `⌂` (local only) rather than `○` (2026-08-16)
- TUI: `esc` hides the expanded help, matching the detail view and the prompts (2026-08-16)
- TUI: status occupies three fixed one-cell slots (dirty, sync, health) instead of concatenated emoji, so a glyph always appears in the same column — previously an up arrow sat in a different place depending on whether the repository was also dirty (2026-08-16)
- TUI: status glyphs are text-presentation (`●↑↓✓○✗`) rather than emoji, which render at inconsistent widths (2026-08-16)
- TUI: column widths come from data known at listing time, so columns no longer shift between the initial render and the loaded one (2026-08-16)
- TUI: the VCS type is an icon left of the name (`📁` directory, `⎇` Git, `ⅉ` Jujutsu), replacing the text column (2026-08-16)
- TUI: the totals moved below the list, behind a rule, and now report the item count alongside the aggregate (2026-08-16)
- TUI rows are width-aligned columns (badge, name, VCS, status). The badge column reports status only: a directory no longer occupies it with a folder icon, since the disclosure marker already identifies it and its type has its own column. Previously the same glyph slot meant "type" on some rows and "status" on others, so it could not be scanned (2026-08-16)
- A clean repository with no remote shows `○` rather than `✅`; it is the one state here with no copy anywhere else, so it no longer borrows the everything-is-fine checkmark (2026-08-16)
- Build-output directories (`node_modules`, `vendor`, `target`, `build`, `dist`, `venv`) are no longer listed at all, rather than being listed with a nested-repo count that recursion then refused to deliver (2026-08-16)
- Batch output is now Name/VCS/Status, listing only noteworthy states, with color on terminals, `tabwriter` alignment, and a summary line (2026-08-16)
- Non-repository directories are hidden unless `--all` is given (2026-08-16)
- With `--filter`, exit 1 when anything matches and 2 for usage errors, so filters work as scriptable checks (2026-08-16)
- All `git`/`jj` subprocesses run through a single helper with timeouts and a non-interactive environment, so a credential prompt fails fast instead of hanging (2026-08-16)
- Per-VCS behavior moved behind a `Backend` interface, selected once from the detected repo type (2026-08-16)

### Added
- TUI: a directory row rolls up the repositories beneath it — `agent-tools  dir  14 repos, 3 dirty, 8 ahead` — computed in the background so the row shows its plain count immediately and fills in as the subtree scan lands (2026-08-16)
- TUI: a total row above the list aggregates the whole scan, standing in for the scan root itself without an indent level; an ellipsis marks a total still waiting on subtree scans (2026-08-16)
- Bulk operations: `--commit-all`, `--pull-all`, `--push-all`, and `--sync-all` act on every eligible repository in scope, composing with `--filter`, `--sort`, `-r`/`--depth`, and the positional directory. Each run prints a plan first, asks `Proceed? [y/N]`, streams one result line per repository, exits 1 if any repository failed, and refuses (exit 2) when stdin is not a terminal without `--yes`/`--dry-run` (2026-08-16)
- `--dry-run` stops after the bulk plan; for `--commit-all` it passes the AI commit tool's own `--dry-run` flag so the would-be messages are shown (2026-08-16)
- `-y`/`--yes` skips the bulk confirmation prompt for scripts and cron (2026-08-16)
- `--commit-all` requires `git-ai-commit` / `jj-ai-commit` on `PATH` and refuses (exit 2, naming the tool and affected repositories) when a needed tool is missing, instead of reusing the single-repo canned message (2026-08-16)
- TUI: `P`/`U`/`C`/`S` run push-all/pull-all/commit-all/sync-all over the eligible repositories in scope, with a y/n confirmation showing the count and first few names, per-row busy state while the batch runs, and rows refreshing as each finishes (2026-08-16)
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
