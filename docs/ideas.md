# gitsync Ideas

This document tracks potential features and improvements for the `gitsync` tool.

See also: [Roadmap](../ROADMAP.md) for planned development milestones.



## Features

### Configuration
- [ ] Support for `~/.config/gitsync/config.yaml` to customize:
  - Which directories to skip/ignore
  - Custom remote names (not just `origin`)
  - Default commit message used by the TUI
  - Output format preferences
  - Default directories to scan
  - Excluding certain directories from the scan
  - Customizing the output format
- [ ] Support for per-repository configuration overrides
- [ ] Support for `.gitsync.yml` project-specific config file

### Repository Detection
- [ ] Detect and report monorepos with nested repositories
- [ ] Option to scan recursively (not just immediate subdirectories)

### Status Information
- [ ] Stash count for Git repos
- [ ] Detect repos with merge conflicts
- [ ] Show repository size on disk

### Output Formats
- [ ] JSON output mode for scripting
- [ ] CSV export option
- [ ] Customizable color themes
- [ ] Compact mode (single line per repo)
- [ ] Verbose mode with additional details

### Performance
- [ ] Caching mechanism for large directory trees

### Filtering and Selection
- [ ] Regex/glob patterns for directory names

### Actions
- [ ] `--fetch` flag to update all remotes before checking
- [ ] `--pull` flag to pull all clean repos
- [ ] `--push` flag to push all repos with ahead commits

## Technical Improvements

### Code Quality
- [ ] Benchmark tests for performance optimization
- [ ] Better error messages with suggested fixes

### Architecture
- [ ] Logging framework with debug levels

### Distribution
- [ ] Homebrew formula
- [ ] GitHub Releases with pre-built binaries

## Documentation
- [ ] Video tutorial/demo
