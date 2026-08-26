# gitsync

[![Go Version](https://img.shields.io/github/go-mod/go-version/osteele/gitsync)](https://go.dev/)
[![Go Report Card](https://goreportcard.com/badge/github.com/osteele/gitsync)](https://goreportcard.com/report/github.com/osteele/gitsync)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![Release](https://img.shields.io/github/v/release/osteele/gitsync?include_prereleases)](https://github.com/osteele/gitsync/releases)

`gitsync` is a command-line tool that scans a directory (or the current one) and reports the version control status of the repositories it finds. It can run as a batch list or as an interactive TUI, scan immediate subdirectories or descend recursively, and act on every repository that needs attention at once.

For each subdirectory, it shows:

-   Whether it's a `git` repository, a `jujutsu` repository, or neither (`dir`). A plain directory that directly contains repositories shows a count, e.g. `3 repos`.
-   Whether the working directory is `dirty` (has uncommitted changes).
-   Whether the repository has a `remote`.
-   Whether there are commits that haven't been pushed to the remote (`ahead`).
-   Whether the remote has commits not present locally (`behind`).

## Features

- **Parallel Processing**: Fast scanning using goroutines for concurrent repository checks
- **Nested Discovery**: Optional recursive descent (`-r`) finds repositories nested inside plain directories, at any depth up to `--depth`
- **Expression-Based Filtering**: Powerful filter expressions to show only the repositories you care about
- **Flexible Sorting**: Sort by multiple fields in any order
- **Smart Defaults**: Automatically detects repository root when run from within a repo
- **Multi-VCS Support**: Works with both Git and Jujutsu repositories
- **Bulk Operations**: Commit, fix Jujutsu history, pull, push, or sync every eligible repository in one plan-first, confirmed run
- **Interactive TUI**: Navigate the directory tree, describe dirty changes, and push, pull, commit, sync, repair, or add a GitHub remote
- **Subtree Rollups**: In the TUI, a directory reports the state of the repositories beneath it, aggregated in the background


## Installation

You can install `gitsync` using `go install`:

```bash
go install github.com/osteele/gitsync@latest
```

## Usage

### Default Behavior

When stdin and stdout are terminals, `gitsync` opens the interactive TUI. If
either stream is redirected, it prints the line-oriented report instead. A
report-specific option such as `--filter`, `--sort`, `--all`, or `--recursive`
also selects the report; use `--list` to request it explicitly, or
`-i`/`--interactive` to request the TUI explicitly.

In either interface, `gitsync` determines which directory to scan as follows:

1. **Inside a repository**: If you're inside a Git or Jujutsu repository, it scans subdirectories of the repository root
2. **Outside a repository**: Scans subdirectories of the current directory

```bash
gitsync
```

To change the automatic default, create
`$XDG_CONFIG_HOME/gitsync/config.toml` (normally
`~/.config/gitsync/config.toml`):

```toml
default_mode = "list"
```

`default_mode` accepts `"tui"` (the built-in default) or `"list"`. The TUI
default still falls back to the report when stdin or stdout is not a terminal.
Command-line mode flags take precedence. Unknown keys and invalid values are
reported as configuration errors.

### Specifying a Directory

To scan a specific directory:

```bash
gitsync ~/code
```

Or:

```bash
gitsync /path/to/directory
```

### Filtering

Use the `--filter` or `-f` flag with expressions to show only specific repositories:

```bash
# Show only dirty repositories
gitsync -f dirty

# Show only Git repositories that are dirty
gitsync -f "git & dirty"

# Show repositories that are either dirty or have unpushed commits
gitsync -f "dirty | ahead"

# Show Git or Jujutsu repos without remotes
gitsync -f "(git | jj) & !remote"

# Show clean repositories with remotes
gitsync -f "clean & remote"
```

#### Filter Terms
- `dirty` - Has uncommitted changes
- `clean` - No uncommitted changes (opposite of dirty)
- `ahead` - Has commits not pushed to remote
- `behind` - Remote has commits not present locally
- `remote` - Has a configured remote
- `local` - No configured remote (opposite of remote)
- `corrupted` - Git repository appears damaged
- `git` - Git repository
- `jj` or `jujutsu` - Jujutsu repository
- `dir` - Not a repository (`bare` is accepted as an alias)

#### Filter Operators
- `&` or `and` - Both conditions must be true
- `|` or `or` - Either condition can be true
- `!` or `not` - Negates the condition
- `()` - Groups conditions

### Sorting

Use the `--sort` or `-s` flag to control the output order:

```bash
# Sort by name (default)
gitsync -s name

# Sort by VCS type (dir, git, jujutsu)
gitsync -s vcs

# Sort with dirty repositories first, then by name
gitsync -s "dirty,name"

# Reverse sort (clean repos first)
gitsync -s "!dirty"

# Complex sort: dirty first, then ahead, then by name
gitsync -s "!dirty,!ahead,name"
```

#### Sort Fields
- `name` - Repository name (alphabetical)
- `vcs` or `type` - Version control system type
- `dirty` - Dirty status
- `ahead` - Unpushed commit count (higher counts first; unknown sorts as 0)
- `behind` - Behind-upstream commit count (higher counts first; unknown sorts as 0)
- `remote` - Remote configuration status

Use `!` prefix to reverse the sort order for a field.

### Recursive Discovery

By default only immediate subdirectories are scanned. Use `-r`/`--recursive` to descend into directories that are not themselves repositories:

```bash
# Find repositories nested at any depth (up to 4 levels)
gitsync -r ~/code

# Cap the descent at 3 levels (--depth implies -r)
gitsync -r --depth 3 ~/code
```

Descent stops as soon as a repository is found, so only the outermost repositories are reported — never a repo nested inside another repo. Directories that contain repositories are listed with their children indented beneath them. Hidden directories (`.`/`_` prefix) and heavy build directories (`node_modules`, `vendor`, `target`, `build`, `dist`, `venv`) are never descended into.

### Bulk Operations

Five flags act on every *eligible* repository in scope at once — exactly the set the equivalent listing command would print, so they compose with `--filter`, `--sort`, `-r`/`--depth`, and the positional directory:

| Flag | Acts on |
|------|---------|
| `--commit-all` | Dirty repositories |
| `--fix-all` | Healthy Jujutsu repositories; runs their configured `jj fix` tools over mutable history |
| `--pull-all` | Repositories with a remote that are behind — or whose behind count is unknown, so a fetch is worthwhile |
| `--push-all` | Repositories with a remote and known unpushed commits |
| `--sync-all` | Repositories eligible for pull or push |

The flags are mutually exclusive with each other and with `-i`.

Every bulk run is plan-first:

```
$ gitsync --push-all
Will push 3 repositories:
  agent-mail      ahead 1
  claude-wrapper  ahead 2
  nib             ahead 1
Proceed? [y/N]
```

- The plan prints before anything happens, then `Proceed? [y/N]` must be answered with `y` or `yes`; anything else aborts.
- `--dry-run` stops after the plan and exits 0 without prompting. For `--commit-all` it goes one step further and passes each AI tool's own `--dry-run` flag, so you see the commit messages that would be used.
- `-y` / `--yes` skips the prompt, for scripts and cron. `--dry-run` wins over `--yes`.
- If stdin is not a terminal and neither `--yes` nor `--dry-run` was given, the run refuses: it prints the plan, explains that confirmation is impossible non-interactively, and exits 2. It never silently proceeds in a pipeline.
- If nothing is eligible it prints e.g. `Nothing to push.` and exits 0.

Repositories normally run concurrently through a bounded worker pool, one result line streams out per repository as it finishes (`✓ agent-mail  pushed` / `✗ nib  push failed: …`), and a summary follows (`2 pushed, 1 failed: nib`). A failure in one repository does not abort the others. For Jujutsu, push first advances the closest local bookmark to the working-copy parent (the built-in-command expansion of `jj tug`), then pushes all bookmarks to the configured remote and verifies that no publishable revisions remain. This catches Jujutsu refusals that otherwise exit successfully. `--fix-all` is deliberately sequential because each `jj fix` may itself launch several formatters. Exit codes: `0` when all succeeded (or nothing was eligible), `1` when any repository failed, `2` for usage errors and refusals.

`--fix-all` shows the effective formatter names reported by `jj config list` in its plan. `--dry-run` is plan-only because `jj fix` has no conventional dry-run. After each repository completes, gitsync prints how many revisions changed, the Jujutsu operation summary, and a reproducing `jj -R PATH op show -p OPERATION` command so the history rewrite can be reviewed or restored through the operation log. The final summary reports revisions changed across repositories changed, separately from failures. Counts come from comparing change and commit IDs before and after the fix operation. The first version intentionally uses each repository's default `jj fix` revset instead of forwarding a cross-repository revset or fileset.

`--commit-all` never reuses the single-repo canned message. It requires `git-ai-commit` (for Git repositories) and `jj-ai-commit` (for Jujutsu repositories) on `PATH`, which generate a conventional-commit message from the diff. Before prompting, gitsync checks that the tools the eligible set actually needs are available; if one is missing it refuses — naming the tool and the affected repositories — and exits 2, rather than partially proceeding.

### Interactive TUI

The interactive terminal UI opens automatically in a terminal. Use `-i` or
`--interactive` to force it (for example, when the configured default is the
list), or `--list` to force the report:

```bash
gitsync -i
gitsync -i ~/code
```

In the TUI you can navigate the directory tree with `↑`/`↓` (or `k`/`j`) and act on the selected repository:

| Key | Action |
|-----|--------|
| `→` / `l` | Expand a directory to reveal the repositories it contains (scanned on first expansion, then cached) |
| `←` / `h` | Collapse an expanded directory |
| `p` | Push the selected repository, or eligible repositories beneath the selected directory rollup |
| `u` | Pull (Git) / fetch (Jujutsu) for the selected repository or directory rollup |
| `f` | Run `jj fix` in the selected Jujutsu repository, or in eligible Jujutsu repositories beneath the selected directory rollup |
| `d` | Show an AI-generated description of the selected repository's dirty changes using `git-ai-commit` or `jj-ai-commit`, when installed |
| `c` | Commit the selected repository, or plan per-repository commits beneath the selected directory rollup |
| `s` | Sync (pull then push) the selected repository or directory rollup |
| `P` | Push all eligible repositories in scope (asks to confirm) |
| `U` | Pull all eligible repositories in scope (asks to confirm) |
| `C` | Commit all dirty repositories with AI-generated messages (asks to confirm) |
| `S` | Sync all eligible repositories in scope (asks to confirm) |
| `r` | Repair a corrupted Git repository (asks to confirm) |
| `a` | Add `origin` from the matching GitHub remote (asks to confirm if replacing one) |
| `o` | Open the repository in `$EDITOR` |
| `v` | Reveal the repository in the system file manager |
| `enter` | Show details for the selected repository, including the full output of its last action |
| `?` | Toggle the key overlay (`esc` hides it) |
| `q` / `ctrl+c` | Quit |

Notes:

- **Confirmations.** `r` and `a` are destructive — `r` moves `.git` aside and `a` can replace an existing `origin` — so they prompt for `y`/`n` first. The shift-key bulk actions (`P`, `U`, `C`, `S`) also confirm first, showing the count and the first few names. Confirmation prompts use sentence capitalization and wrap within the terminal while reserving their full height, so target names and `(y/n)` remain visible. `esc` also cancels.
- **Commit messages.** `c` opens an expanding multiline editor that shows the complete generated subject and body when they fit. `enter` commits; `esc` cancels without discarding a generated draft from the session cache.
- **Change descriptions.** `d` is available only for a dirty repository whose matching `git-ai-commit` or `jj-ai-commit` executable is on `PATH`. It invokes the tool with `--dry-run --json` and validates the versioned result before displaying it. When the generated description, summary, and file list fit, they share one page. Otherwise they become separate views: `←`/`→` (or `h`/`l`, tab/shift-tab) switches views, and page-up/page-down scrolls long content. The model, tool, target, file count, and diff size remain in a pinned line. Descriptions generated from either `c` or `d` are cached in memory for the current TUI session, per repository and diff hash; reopening either view reuses the description while the tree is unchanged. Changing the diff, successfully committing, or quitting gitsync discards that reuse. In the description view, `c` commits with the generated description and returns to the list; `esc` returns without committing.
- **Help.** The short key line is anchored to the terminal's bottom row, so expanding and collapsing directories do not move it. `?` opens a modal key overlay over the list, so showing help does not resize or scroll the repository view. While it is open, `?` and `esc` close it and other keys are ignored.
- **One action at a time per repository.** While an action runs, its row shows `⏳` and further keys for that repository are ignored; other repositories remain available.
- **Bulk progress and failures.** While a bulk action runs, its status uses the active verb (`Pushing`, `Pulling`, `Committing`, `Fixing`, or `Syncing`) and reports completed/total, actual worker starts by repository name, queued work, failures, and elapsed time. A failed batch automatically opens a pageable report with a numbered record for each failure: repository name, location, version-control system, and complete error output are labeled separately. For a directory-scoped action, that report remains available from the directory's `enter` details after returning to the list. Other successful results clear after a few seconds; failures remain available.
- **Rollups.** A directory row reports the state of every repository beneath it. When repositories occur below another plain directory, the count makes that explicit (`42 repos (31 direct + 11 nested), 15 dirty`). Repositories whose ahead count could not be determined count as `ahead ?` rather than as synced; for example, `1 ahead ?` means one repository has an unknown count, not one unknown commit. A total row below the list aggregates the whole scan. Both are computed in the background: rows appear immediately with a plain count and fill in as the subtree scans land, so nothing blocks on them. Rollups are TUI-only — the batch listing stays a fast shallow scan.
- **Rollup actions.** Lowercase `p`, `u`, `c`, `s`, and `f` operate on the selected row: one repository directly, or a confirmed plan for eligible repositories recursively beneath one selected directory rollup. `f` selects only healthy Jujutsu repositories, runs their configured `jj fix` tools sequentially, and reports revisions changed across repositories changed. Uppercase `P`, `U`, `C`, and `S` instead operate across the entire scan root, including repositories represented by collapsed rollups. After an action completes, affected directory rollups and the total temporarily return to a pending state and are recomputed, so they never continue presenting pre-action counts as current.
- **Columns.** Each row is `status · type · name · detail`, and every column has a width fixed by data known at listing time, so nothing shifts as background scans land. The nesting indent leads the whole row, so a child's icons sit beneath its parent's while the detail column stays aligned. A rule and a `Total` line close the table with the item count and the aggregate.
- **Badges.** Status occupies three fixed one-cell slots, so a glyph always appears in the same column: `●` dirty, then `↑` ahead / `↓` behind, then `✓` clean and backed up / `⌂` local only (no remote) / `✗` corrupted / `!` scan error / `⋯` action running. Text-presentation glyphs are used throughout because emoji render at inconsistent widths, which is what makes columns wander.
- **Type icon.** Left of the name: `📁` a directory, `⎇` a Git repository, `ⅉ` a Jujutsu repository. The two repository glyphs are from different families on purpose — a letterform beside line art is told apart at a glance, where a second fork-shaped mark would not be.

The list scrolls when there are more repositories than fit on screen, and the status of each updates in the background.

#### GitHub remote setup

The `a` key uses the GitHub REST API to find a repository named after the local directory under your authenticated user. Set a token in your environment:

```bash
export GITHUB_TOKEN=ghp_...
```

The token needs only `repo` or `public_repo` scope for private or public repositories respectively.

### Output

The output is a table with three columns:

- **Name**: The name of the subdirectory, indented one level per nesting depth under a container directory.
- **VCS**: The version control system: `git`, `jujutsu`, or `dir` (not a repository).
- **Status**: Only the noteworthy states, so a healthy repository stays quiet. Possible tokens:

| Token | Meaning |
|-------|---------|
| `error: …` | The repository could not be scanned |
| `corrupted` | The Git repository appears damaged |
| `dirty` | There are uncommitted changes |
| `ahead N` | `N` local commits have not been pushed |
| `behind N` | The remote has `N` commits not present locally |
| `ahead ?` | A remote exists but the count could not be determined (for Git, commonly because no upstream is configured) |
| `no remote` | No remote is configured |
| `N repos` | A plain directory directly containing `N` repositories |
| `clean` | None of the above |

`error` and `corrupted` suppress the other tokens, since the rest of the status could not be determined reliably. When the output is a terminal, tokens are colored by severity; set `NO_COLOR` to disable.

A summary line follows the table.

### Example Output

```
$ gitsync
Name                 VCS      Status
coffee-shop-finder   git      clean
todo-app-but-better  git      dirty ahead 3
my-awesome-blog      jujutsu  clean
cat-meme-generator   jujutsu  no remote
dotfiles             git      dirty
random-excuse-api    git      ahead 1
broken-checkout      git      corrupted

6 repos (+1 non-repo hidden): 2 dirty, 2 ahead, 1 no remote, 1 corrupted
```

Non-repository directories are hidden by default, unless they contain repositories — those are shown with their count (e.g. `18 repos`) so nested work is never invisible. Use `--all` to include the rest:

```
$ gitsync --all
Name             VCS   Status
old-experiments  dir
...
```

### Scripting

Exit codes:

| Code | Meaning |
|------|---------|
| `0` | Success; with `--filter`, nothing matched |
| `1` | With `--filter`, at least one repository matched |
| `2` | Usage error (bad filter or sort expression) |

So a filter doubles as a check — the `&&` branch runs only when nothing needs attention:

```bash
gitsync -f "dirty | ahead" && echo "everything is in sync"
```

Color is disabled automatically when the output is not a terminal, so piping to a file or another program yields plain text.

### Combined Examples

```bash
# Show only dirty Git repos, sorted by name
gitsync -f "git & dirty" -s name

# Show repos needing attention, with most urgent first
gitsync -f "dirty | ahead" -s "!dirty,!ahead,name"

# Find all local repos without remotes
gitsync -f "!remote & (git | jj)"

# Show Jujutsu repos that have unpushed changes
gitsync -f "jj & (dirty | ahead)"
```

## Comparison with Similar Tools

`gitsync` focuses on providing a quick overview of multiple repositories' VCS status in a single view. Here's how it compares to other tools:

- **`git status` / `jj status`**: These show detailed status for a single repository. `gitsync` shows summary status for multiple repositories at once.
- **`mr` (myrepos)**: A more complex tool for managing multiple repositories with support for various VCS and custom commands. Supports Git, SVN, Mercurial, and others through plugins, but not Jujutsu. `gitsync` is simpler and focused primarily on status reporting.
- **`gita`**: Python tool for managing multiple git repos with colored output and group operations. Git-only, no Jujutsu support. `gitsync` is distributed as a single Go binary and supports both Git and Jujutsu.
- **`multi-git-status`**: Bash script showing git status across repos. Git-only, no Jujutsu support. `gitsync` adds Jujutsu support and provides a cleaner table output.
- **`git-xargs`**: Focused on running commands across multiple repos. Git-only, no Jujutsu support. `gitsync` focuses on status visualization with planned interactive features.

`gitsync` is designed as a fast tool to quickly see which of your local repositories need attention (uncommitted changes, unpushed commits, etc.), especially if you work with both Git and Jujutsu repositories.

## See Also

For more Jujutsu development tools, see [my collection of version control utilities](https://osteele.com/software/development-tools/#version-control).

## Development

For instructions on how to contribute to `gitsync`, see [DEVELOPMENT.md](DEVELOPMENT.md).

# LICENSE

MIT License
