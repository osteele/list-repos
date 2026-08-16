# Roadmap

This file outlines the future development milestones for the `gitsync` CLI.

See also: [Ideas](docs/ideas.md) for additional feature ideas and improvements.

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
- Count of unpulled commits for Jujutsu repositories (Git already reports `behind N`)

## Milestone: Worktrees and Workspaces

- Detect and display Git worktrees and Jujutsu workspaces
  - Show worktrees/workspaces indented under main local repo when both exist
  - Indicate which workspace/worktree is active
  - Group related worktrees/workspaces together in output

## Milestone: Background Fetching and Remote Status

- Implement a background process that periodically runs `git fetch` for all detected Git repositories.
- The CLI will then display whether the remote repository has changes that are not present locally (i.e., if `origin/main` is ahead of `main`).
- This will provide a more complete picture of the repository's sync status without requiring the user to manually fetch remotes.

