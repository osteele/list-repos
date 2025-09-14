# list-repos Wishlist

This document tracks potential features and improvements for the `list-repos` tool.

## Current Limitations (High Priority)

These are known limitations that should be addressed soon:

- [ ] **Branch detection**: Currently hardcoded to `main` branch for ahead commit detection
  - Need to detect the actual default branch (could be `master`, `main`, `develop`, etc.)
  - Should check current branch tracking information
- [ ] **Git ahead detection fails silently**: When `origin/main` doesn't exist, assumes no ahead commits
  - Should detect the actual tracked branch
  - Should handle repos without upstream branches properly
- [ ] **No configuration support**: Cannot customize branch names or remote names
  - Implement config file support for custom defaults
- [ ] **Integration tests are placeholder only**: Need actual integration tests
  - Test the CLI with real repository scenarios
  - Test edge cases and error conditions

## Features

### Configuration
- [ ] Support for `.list-repos.yml` or similar config file to customize:
  - Default branch names (not just `main`) - *addresses current limitation*
  - Which directories to skip/ignore
  - Custom remote names (not just `origin`) - *addresses current limitation*
  - Output format preferences
- [ ] Auto-detect default branch from git/jj configuration
- [ ] Support for per-repository configuration overrides

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
- [ ] Colored output with customizable themes
- [ ] Compact mode (single line per repo)
- [ ] Verbose mode with additional details
- [ ] Sort options (by name, status, last modified, etc.)

### Performance
- [ ] Parallel repository scanning for faster execution
- [ ] Caching mechanism for large directory trees
- [ ] Progress indicator for slow scans

### Filtering and Selection
- [ ] Filter by repository type (`--only-git`, `--only-jj`)
- [ ] Filter by status (`--only-dirty`, `--only-ahead`)
- [ ] Regex/glob patterns for directory names
- [ ] Interactive mode to select repos for batch operations

### Actions
- [ ] `--fetch` flag to update all remotes before checking
- [ ] `--pull` flag to pull all clean repos
- [ ] `--push` flag to push all repos with ahead commits
- [ ] Generate summary report with statistics

## Technical Improvements

### Code Quality
- [ ] Add comprehensive integration tests
- [ ] Benchmark tests for performance optimization
- [ ] Error recovery for individual repo failures
- [ ] Better error messages with suggested fixes

### Architecture
- [ ] Logging framework with debug levels

### Distribution
- [ ] Homebrew formula
- [ ] GitHub Releases with pre-built binaries

## Documentation
- [ ] Video tutorial/demo
- [ ] Comparison with similar tools
