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
- A repository is considered dirty if `jj diff --summary` returns any output

### Remote Status

**Git Repositories**
- A repository has a remote if `git remote` returns any output
- This checks for any configured remotes, not just `origin`

**Jujutsu Repositories**
- A repository has a remote if `jj git remote list` returns any output
- This checks for Git remotes configured in the Jujutsu repository

### Unpushed Status

**Git Repositories**
- The ahead count is `git rev-list --count @{u}..HEAD`, against the current
  branch's configured upstream; no branch name is assumed
- A missing upstream or failed query produces an unknown count rather than zero

**Jujutsu Repositories**
- The ahead count is the number of non-empty revisions reachable from local
  bookmarks or the working-copy parent but not reachable from remote bookmarks
- This matches push behavior: push advances the closest local bookmark to the
  working-copy parent, pushes all bookmarks to the configured remote, then
  verifies that the ahead query is zero
- A failed query produces an unknown count rather than zero

## Output Format

### Interface Selection

- With terminal stdin and stdout, the TUI is the default. A redirected stream
  selects the line-oriented report.
- `-i`/`--interactive` explicitly selects the TUI; `--list` explicitly selects
  the report. Report-specific flags also select the report. Bulk operations
  always use their line-oriented, plan-first interface.
- The XDG config file `gitsync/config.toml` may set `default_mode` to `tui` or
  `list`. Explicit command-line selection takes precedence, and non-terminal
  streams still prevent automatic TUI selection.

### TUI Rollups

- A directory rollup counts every outermost repository found recursively
  beneath that directory, not merely its immediate child directories.
- When any counted repository is below another plain directory, the rollup
  separates direct and nested counts so the recursive total is not mistaken
  for an immediate-child count.
- Expanded child rows do not add to the total a second time; each top-level
  directory contributes its rollup once.
- Lowercase repository actions (`p`, `u`, `c`, `s`, and `f`) operate on the
  selected row. For a directory rollup they recursively scan only that
  directory, apply normal eligibility rules, and present a confirmed plan.
  `f` runs `jj fix` only for healthy Jujutsu repositories, sequentially when
  the selected directory contains more than one. Fix compares revision
  identities across the resulting Jujutsu operation and reports revisions
  changed across repositories changed. Uppercase actions recursively
  scan the whole display scope, including repositories represented by collapsed
  rows, before presenting their confirmation plan.
- Completion of a repository or bulk action invalidates every visible ancestor
  rollup affected by it. Those rollups and the total remain explicitly pending
  until a post-action scan completes; sequence numbers prevent an older scan
  from restoring stale counts.
- A running bulk action reports its active verb, completed and total counts,
  repositories actually started by workers, queued count, failures, and elapsed
  time. A failed batch opens a pageable report containing each repository's
  complete error and retains that report on a selected directory rollup. Each
  failure explicitly labels its ordinal, repository name, location, VCS, and
  error so a repository basename cannot be mistaken for a tool phase.
- Confirmation prompts begin with a capitalized action and wrap to the terminal
  width. Their complete wrapped height is reserved from the repository list.
- `d` runs the installed VCS-specific AI commit tool with `--dry-run --json`
  and accepts only a valid `ai-describe/v1` result. The display is read-only
  until the user explicitly presses `c`.
- The description display combines the generated description, summary, and
  changed files when their wrapped content fits. Otherwise each section has
  an independently scrollable view, with keys to move between views and page
  through long sections. A single pinned line carries the active view, model,
  tool, target, file count, and diff size wherever space permits.
- In the description display, `c` commits with the generated description and
  returns to the list; `esc` returns without committing.
- Generated descriptions are cached only in the running TUI model. Each
  repository's entry is keyed by a SHA-256 digest of the Git or Jujutsu diff
  input. Generation from either the description view or the ordinary commit
  prompt warms the same entry. Canceling a commit prompt retains it; reopening
  either view reuses it only while that digest matches. A changed tree or a
  successful commit invalidates the entry, and quitting drops the cache.
- The ordinary commit prompt uses an expanding multiline editor so a generated
  subject and body remain visible together when terminal space permits.
- The expanded key help is a modal overlay and does not alter list height or
  navigation state.

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
