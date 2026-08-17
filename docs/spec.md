# gitsync Specifications

## Directory Scanning

- By default, only immediate subdirectories of the target path are scanned
- `-r`/`--recursive` descends; `--depth N` caps the descent and implies `-r`
- Descent enters only directories that are **not** themselves repositories, and stops
  at the first repository found. Only the outermost repositories are reported, never a
  repository nested inside another one. This is what allows repositories to be found at
  differing depths without a fixed level
- Hidden directories (starting with `.`) are excluded from the scan
- Directories starting with `_` are excluded from the scan
- `node_modules`, `vendor`, `target`, `build`, `dist`, and `venv` are never descended into
- Non-directory entries are ignored
- All scanned directories are included in the output, including plain directories,
  though plain directories are hidden unless `--all` is given

### Design decision: symlinks are not followed

Directory entries that are symbolic links are **not** treated as directories, so a
repository reachable only through a symlink is not reported. This follows the
`os.ReadDir`/`entry.IsDir()` semantics the scanner is built on, where a symlink reports
its own type rather than its target's.

This is deliberate rather than an oversight. Following symlinks would require two
additional mechanisms that a status tool does not otherwise need:

- **Deduplication by resolved path.** Symlinks commonly alias a sibling within the same
  tree (`~/code/claude-hooks -> ~/code/agent-tools/claude-hooks`). Following them would
  report the same repository more than once, and bulk operations would then act on it
  twice.
- **Cycle detection.** Under `-r`, a symlink pointing at an ancestor produces an
  unbounded walk, so recursion would need a visited-set keyed on resolved real paths.

The visible cost is that a container's repository count can be lower than the number of
child directories a shell would show: `~/code/agent-tools` reports 14 repositories while
holding 17, because 3 are symlinks. Revisit this if aliased repositories become common
enough that their absence is more surprising than duplicates would be.

## Repository Detection

### Repository Types

- **Jujutsu**: Directory contains `.jj` subdirectory (takes precedence)
- **Git**: Directory contains `.git` subdirectory and does NOT contain `.jj` subdirectory
- **Dir**: Directory contains neither `.git` nor `.jj` subdirectories

`dir` is displayed for the third case. It is deliberately not called `bare`: in Git, a
*bare repository* is a real repository without a working tree, which is close to the
opposite of "not a repository at all". `bare` remains accepted as a filter alias for
backward compatibility. A plain directory reports how many repositories it directly
contains, so a container is distinguishable from an empty directory without recursion.

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
- Three columns: Name, VCS, Status, width-aligned to their contents via `tabwriter`
- Repository name shown as the basename of the directory path, indented one level per
  nesting depth beneath a container directory
- The Status column lists only noteworthy states, so a healthy repository stays quiet.
  `error` and `corrupted` suppress the other tokens, since the rest of the status could
  not be determined reliably
- Counts are rendered as numbers (`ahead 3`); a count that could not be determined
  renders as `?` rather than as zero
- Tokens are colored by severity when stdout is a terminal and `NO_COLOR` is unset;
  otherwise output is plain text
- A summary line follows the table

### Repository Display
- Plain directories are hidden unless `--all` is given; the summary reports how many
  were hidden
- A parent kept only as context for a matched child is shown but not counted in the
  summary tallies
- Directories that cause errors during status detection are **displayed**, carrying an
  `error` status. A directory that cannot be scanned is precisely the one the user needs
  to see, so it is never dropped from the table
- A scan error is distinct from corruption. Only genuine damage sets `corrupted`, since
  that state invites the repair action, which moves `.git` aside