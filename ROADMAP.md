# Roadmap

This file outlines the future development milestones for the `dir-status` CLI.

## Milestone 0: Performance Optimization

- Collect directory status in parallel using goroutines
- Implement concurrent execution for VCS command calls
- Add progress indicator for large directory scans

## Milestone 1: CLI Filtering and Sorting Options

- Add command-line flags for filtering displayed repositories:
  - `--dirty` - Show only repositories with uncommitted changes
  - `--unpushed` - Show only repositories with unpushed commits
  - `--git` - Show only Git repositories
  - `--jj` - Show only Jujutsu repositories
  - `--no-repo` - Show directories that are not repositories
  - `--has-remote` - Show only repositories with configured remotes
  - `--no-remote` - Show only repositories without configured remotes
- Support combining filters (e.g., `--dirty --git` for dirty Git repos)
- Add sorting options:
  - `--sort=dirty` - Sort by dirty status (dirty repos first)
  - `--sort=ahead` - Sort by ahead status (repos with unpushed commits first)
  - `--sort=name` - Sort alphabetically by name (default)
  - `--sort=vcs` - Sort by VCS type (git, then jujutsu, then bare)

## Milestone 2: Enhanced Status Information

- Show the current branch name for Git repositories
- Show ancestor bookmark(s) for Jujutsu repositories
- Display last commit date/age for each repository
- Count of uncommitted files (not just dirty flag)
- Count of unpushed commits (exact number)
- Count of unpulled commits (remote ahead of local)

## Milestone 3: Worktrees and Workspaces

- Detect and display Git worktrees and Jujutsu workspaces
  - Show worktrees/workspaces indented under main local repo when both exist
  - Indicate which workspace/worktree is active
  - Group related worktrees/workspaces together in output

## Milestone 4: Interactive TUI

- Implement a terminal user interface (TUI) using a library like `bubbletea` or `tview`.
- The TUI will display the list of repositories and their statuses in a structured and interactive way.
- Users will be able to select one or more repositories from the list.
- Add functionality to:
  - Push selected repositories
  - Pull selected repositories
  - Tug selected jj repositories
  - Fold or unfold repositories that have

## Milestone 5: Background Fetching and Remote Status

- Implement a background process that periodically runs `git fetch` for all detected Git repositories.
- The CLI will then display whether the remote repository has changes that are not present locally (i.e., if `origin/main` is ahead of `main`).
- This will provide a more complete picture of the repository's sync status without requiring the user to manually fetch remotes.

## Milestone 6: Configuration

- Add a configuration file (e.g., `~/.config/dir-status/config.yaml`) to allow users to customize the tool's behavior.
- Configuration options could include:
    - Specifying default directories to scan.
    - Excluding certain directories from the scan.
    - Customizing the output format.
