# list-repos

[![Go Version](https://img.shields.io/github/go-mod/go-version/osteele/list-repos)](https://go.dev/)
[![Go Report Card](https://goreportcard.com/badge/github.com/osteele/list-repos)](https://goreportcard.com/report/github.com/osteele/list-repos)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![Release](https://img.shields.io/github/v/release/osteele/list-repos?include_prereleases)](https://github.com/osteele/list-repos/releases)

`list-repos` is a command-line tool that scans the immediate subdirectories of a specified path (or the current directory) and displays their version control status.

For each subdirectory, it shows:

-   Whether it's a `git` repository, a `jujutsu` repository, or neither (`bare`).
-   For git repositories, whether the working directory is `dirty` (has uncommitted changes).
-   Whether the repository has a `remote`.
-   Whether there are commits that haven't been pushed to the `origin` remote.


## Installation

You can install `list-repos` using `go install`:

```bash
go install github.com/osteele/list-repos@latest
```

## Usage

### Default Behavior

When run without arguments, `list-repos` intelligently determines which directory to scan:

1. **Inside a repository**: If you're inside a Git or Jujutsu repository, it scans subdirectories of the repository root
2. **Outside a repository**: Scans subdirectories of the current directory

```bash
list-repos
```

### Specifying a Directory

To scan a specific directory:

```bash
list-repos ~/code
```

Or:

```bash
list-repos /path/to/directory
```

### Output

The output is a table with the following columns:

- **Name**: The name of the subdirectory.
- **VCS**: The version control system: `git`, `jujutsu`, or `bare`.
- **Dirty**: `✓` if there are uncommitted changes, `✗` otherwise.
- **Remote**: `✓` if a remote is configured, `✗` otherwise.
- **Ahead**: `✓` if there are local commits that haven't been pushed to the `origin` remote, `✗` otherwise.

### Example Output

```
$ list-repos
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
$ list-repos --no-unicode
Name                           VCS        Dirty   Remote  Ahead
coffee-shop-finder             git        false   true    false
todo-app-but-better            git        true    true    true
my-awesome-blog                jujutsu    false   true    false
cat-meme-generator             jujutsu    false   false   false
```

## Comparison with Similar Tools

`list-repos` focuses on providing a quick overview of multiple repositories' VCS status in a single view. Here's how it compares to other tools:

- **`git status` / `jj status`**: These show detailed status for a single repository. `list-repos` shows summary status for multiple repositories at once.
- **`mr` (myrepos)**: A more complex tool for managing multiple repositories with support for various VCS and custom commands. Supports Git, SVN, Mercurial, and others through plugins, but not Jujutsu. `list-repos` is simpler and focused primarily on status reporting.
- **`gita`**: Python tool for managing multiple git repos with colored output and group operations. Git-only, no Jujutsu support. `list-repos` is distributed as a single Go binary and supports both Git and Jujutsu.
- **`multi-git-status`**: Bash script showing git status across repos. Git-only, no Jujutsu support. `list-repos` adds Jujutsu support and provides a cleaner table output.
- **`git-xargs`**: Focused on running commands across multiple repos. Git-only, no Jujutsu support. `list-repos` focuses on status visualization with planned interactive features.

`list-repos` is designed as a fast tool to quickly see which of your local repositories need attention (uncommitted changes, unpushed commits, etc.), especially if you work with both Git and Jujutsu repositories.

## See Also

For more Jujutsu development tools, see [my collection of version control utilities](https://osteele.com/software/development-tools/#version-control).

## Development

For instructions on how to contribute to `list-repos`, see [DEVELOPMENT.md](DEVELOPMENT.md).

# LICENSE

MIT License
