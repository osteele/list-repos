# gitsync

[![Go Version](https://img.shields.io/github/go-mod/go-version/osteele/gitsync)](https://go.dev/)
[![Go Report Card](https://goreportcard.com/badge/github.com/osteele/gitsync)](https://goreportcard.com/report/github.com/osteele/gitsync)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![Release](https://img.shields.io/github/v/release/osteele/gitsync?include_prereleases)](https://github.com/osteele/gitsync/releases)

`gitsync` is a command-line tool that scans the immediate subdirectories of a specified path (or the current directory) and displays their version control status.

For each subdirectory, it shows:

-   Whether it's a `git` repository, a `jujutsu` repository, or neither (`bare`).
-   For git repositories, whether the working directory is `dirty` (has uncommitted changes).
-   Whether the repository has a `remote`.
-   Whether there are commits that haven't been pushed to the `origin` remote.

## Features

- **Parallel Processing**: Fast scanning using goroutines for concurrent repository checks
- **Expression-Based Filtering**: Powerful filter expressions to show only the repositories you care about
- **Flexible Sorting**: Sort by multiple fields in any order
- **Smart Defaults**: Automatically detects repository root when run from within a repo
- **Multi-VCS Support**: Works with both Git and Jujutsu repositories


## Installation

You can install `gitsync` using `go install`:

```bash
go install github.com/osteele/gitsync@latest
```

## Usage

### Default Behavior

When run without arguments, `gitsync` intelligently determines which directory to scan:

1. **Inside a repository**: If you're inside a Git or Jujutsu repository, it scans subdirectories of the repository root
2. **Outside a repository**: Scans subdirectories of the current directory

```bash
gitsync
```

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
- `remote` - Has a configured remote
- `local` - No configured remote (opposite of remote)
- `git` - Git repository
- `jj` or `jujutsu` - Jujutsu repository
- `bare` - Not a repository

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

# Sort by VCS type (Bare, Git, Jujutsu)
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
- `ahead` - Ahead commits status
- `remote` - Remote configuration status

Use `!` prefix to reverse the sort order for a field.

### Output

The output is a table with the following columns:

- **Name**: The name of the subdirectory.
- **VCS**: The version control system: `git`, `jujutsu`, or `bare`.
- **Dirty**: `✓` if there are uncommitted changes, `✗` otherwise.
- **Remote**: `✓` if a remote is configured, `✗` otherwise.
- **Ahead**: `✓` if there are local commits that haven't been pushed to the `origin` remote, `✗` otherwise.

### Example Output

```
$ gitsync
Name                           VCS        Dirty   Remote  Ahead
coffee-shop-finder             git        ✗       ✓       ✗
todo-app-but-better            git        ✓       ✓       ✓
my-awesome-blog                jujutsu    ✗       ✓       ✗
cat-meme-generator             jujutsu    ✗       ✗       ✗
dotfiles                       git        ✓       ✓       ✗
random-excuse-api              git        ✗       ✓       ✓
old-experiments                bare       ✗       ✗       ✗
```

To use text instead of Unicode symbols, use the `--no-unicode` flag:

```
$ gitsync --no-unicode
Name                           VCS        Dirty   Remote  Ahead
coffee-shop-finder             git        false   true    false
todo-app-but-better            git        true    true    true
my-awesome-blog                jujutsu    false   true    false
cat-meme-generator             jujutsu    false   false   false
```

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
