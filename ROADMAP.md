# Roadmap

This file outlines the future development milestones for the `gitsync` CLI.

See also: [Ideas](docs/ideas.md) for additional feature ideas and improvements.

## Milestone: Performance Optimization ✅

- ✅ Collect directory status in parallel using goroutines
- ✅ Implement concurrent execution for VCS command calls
- Add progress indicator for large directory scans

## Milestone: Fix Branch Detection and Ahead Status

Address critical issues with branch detection and ahead status:

- **Branch detection**: Currently hardcoded to `main` branch for ahead commit detection
  - Detect the actual default branch (could be `master`, `main`, `develop`, etc.)
  - Check current branch tracking information
- **Git ahead detection fails silently**: When `origin/main` doesn't exist, assumes no ahead commits
  - Detect the actual tracked branch
  - Handle repos without upstream branches properly

## Milestone: Expression-Based Filtering and Sorting ✅

✅ Implement a unified expression language for filtering and sorting that scales from CLI to future TUI:

### Filter Expression System
- **Basic syntax:** `gitsync [--filter|-f EXPR] [--sort|-s EXPR]`
- **Filter terms:** `dirty`, `clean`, `ahead`, `remote`, `local`, `git`, `jj`, `bare`
- **Operators:** `and`/`&`, `or`/`|`, `not`/`!`, with parentheses for grouping
- **Examples:**
  - `gitsync -f "dirty"` - Only dirty repos
  - `gitsync -f "git & dirty"` - Dirty git repos
  - `gitsync -f "dirty | ahead"` - Dirty OR has unpushed commits
  - `gitsync -f "(git | jj) & !remote"` - VCS repos without remotes

### Sort Expression System
- **Single field:** `gitsync -s name` (default)
- **Multiple fields:** `gitsync -s "vcs,name"`
- **Reverse order:** `gitsync -s "!dirty,name"` (dirty first, then by name)
- **Sort fields:** `name`, `vcs`, `dirty`, `ahead`, `modified` (future)

### Configuration File with Aliases
- Support for `~/.config/gitsync/config.yaml`
- Define filter aliases for common queries
- Example: `gitsync -f @work` to use predefined work filter

### Benefits
- Single unified interface instead of multiple flags
- Composable expressions for complex queries
- Natural extension path to TUI with interactive filter bar
- Familiar syntax similar to other developer tools

## Milestone: Integration Tests

- Implement comprehensive integration tests
- Test the CLI with real repository scenarios
- Test edge cases and error conditions
- Test parallel processing performance

## Milestone: Enhanced Status Information

- Show the current branch name for Git repositories
- Show ancestor bookmark(s) for Jujutsu repositories
- Display last commit date/age for each repository
- Count of uncommitted files (not just dirty flag)
- Count of unpushed commits (exact number)
- Count of unpulled commits (remote ahead of local)

## Milestone: Worktrees and Workspaces

- Detect and display Git worktrees and Jujutsu workspaces
  - Show worktrees/workspaces indented under main local repo when both exist
  - Indicate which workspace/worktree is active
  - Group related worktrees/workspaces together in output

## Milestone: Interactive TUI ✅

- ✅ Implement a terminal user interface (TUI) using `bubbletea`.
- ✅ Display the list of repositories and their statuses in a structured and interactive way.
- ✅ Users can select a repository and run actions on it.
- ✅ Add functionality to:
  - Push selected repositories
  - Pull selected repositories
  - Commit changes in selected repositories
  - Sync selected repositories (pull then push)
  - Repair corrupted Git repositories
  - Add a GitHub remote to selected repositories

## Milestone: Background Fetching and Remote Status

- Implement a background process that periodically runs `git fetch` for all detected Git repositories.
- The CLI will then display whether the remote repository has changes that are not present locally (i.e., if `origin/main` is ahead of `main`).
- This will provide a more complete picture of the repository's sync status without requiring the user to manually fetch remotes.

