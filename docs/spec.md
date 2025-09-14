# dir-status Specifications

## Directory Scanning

- Only immediate subdirectories of the target path are scanned (non-recursive)
- Hidden directories (starting with `.`) are excluded from the scan
- Directories starting with `_` are excluded from the scan
- Non-directory entries are ignored
- All scanned directories are included in the output, including bare directories

## Repository Detection

### Repository Types

- **Jujutsu**: Directory contains `.jj` subdirectory (takes precedence)
- **Git**: Directory contains `.git` subdirectory and does NOT contain `.jj` subdirectory
- **Bare**: Directory contains neither `.git` nor `.jj` subdirectories

### VCS Priority

When a directory contains both `.git` and `.jj` directories, it is treated as a Jujutsu repository. This ensures that Jujutsu colocated repositories (which maintain both `.git` and `.jj` directories) are correctly identified and their status is reported using Jujutsu commands rather than Git commands.

## Status Detection

### Dirty Status

**Git Repositories**
- A repository is considered dirty if `git status --porcelain` returns any output
- This includes untracked files, modified files, and staged changes

**Jujutsu Repositories**
- A repository is considered dirty if the current revision has files and no description
- Checked via `jj status` output

### Remote Status

**Git Repositories**
- A repository has a remote if `git remote` returns any output
- This checks for any configured remotes, not just `origin`

**Jujutsu Repositories**
- A repository has a remote if `jj git remote list` returns any output
- This checks for Git remotes configured in the Jujutsu repository

### Unpushed Status

**Git Repositories**
- A repository has unpushed commits if `git log origin/main..main` returns any output
- This assumes the main branch is named `main`
- If `origin/main` doesn't exist, the repository is considered to have no unpushed commits
- Error from the command is silently ignored (returns false for unpushed)

**Jujutsu Repositories**
- A repository has unpushed commits if `jj log -r "all() & ~ remote_branches()"` returns revisions
- The command output is checked for "(empty)" - if present, no unpushed commits exist
- If the command fails, the repository is considered to have no unpushed commits

## Output Format

### Table Layout
- Column widths: Name (30), VCS (10), Dirty (7), Remote (7), Unpushed (7)
- Left-aligned with space padding
- Boolean values displayed as Unicode symbols by default:
  - `✓` for true
  - `✗` for false
- With `--no-unicode` flag, boolean values displayed as text:
  - `true` for true
  - `false` for false
- Repository name shown as basename of the directory path

### Repository Display
- All scanned directories are displayed, including bare directories
- Bare directories show "bare" as VCS type with false for all status flags
- Directories that cause errors during status detection are silently skipped