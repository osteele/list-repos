# Development Guide

This guide covers the technical aspects of developing `list-repos`.

## Development Setup

1. **Prerequisites**
   - Go 1.23 or later
   - [just](https://github.com/casey/just) command runner
   - Git and Jujutsu (jj) CLI tools for running tests

2. **Getting Started**
   ```bash
   # Clone the repository
   git clone https://github.com/osteele/list-repos.git
   cd list-repos

   # Install dependencies and git hooks
   just setup

   # Run all checks to verify setup
   just check
   ```

   The `just setup` command will:
   - Install Go dependencies
   - Install development tools from `tools.go` (golangci-lint, lefthook)
   - Set up git hooks for automatic code quality checks

   Development tools are managed in `tools.go` to ensure consistent versions across all environments.

## Available Commands

Run `just` to see all available commands:

- `just setup` - Install and update dependencies
- `just check` - Run all quality checks (format, lint, test)
- `just format` - Format code using go fmt
- `just lint` - Run linting checks
- `just fix` - Auto-fix formatting and linting issues
- `just test` - Run unit tests
- `just test-coverage` - Run tests with coverage report
- `just integration-test` - Run integration tests
- `just build` - Build the binary
- `just run` - Run the application
- `just clean` - Clean build artifacts

## Project Structure

```
list-repos/
├── main.go           # Main application logic
├── main_test.go      # Unit tests
├── integration_test.go # Integration tests (placeholder)
├── go.mod           # Go module definition
├── justfile         # Task runner configuration
└── docs/
    └── wishlist.md  # Feature roadmap
```

## Architecture

The application follows a simple architecture with all logic in `main.go`:

1. **RepoType Enum**: Identifies repository types (Bare, Git, Jujutsu)
2. **RepoStatus Struct**: Holds repository state (path, type, dirty, remote, ahead)
3. **Main Flow**:
   - Parse CLI arguments or detect default directory
   - Scan subdirectories of target directory
   - Detect repository type by checking for `.git` or `.jj` directories
   - Query VCS status using command execution
   - Display formatted table of results

## Testing

### Running Tests

```bash
# Run all tests
just test

# Run specific test
go test -run TestGetGitStatus

# Run with coverage
just test-coverage

# Run integration tests
just integration-test
```

### Test Structure

Tests use temporary directories and real VCS commands:
- `TestGetSubdirectories`: Verifies directory scanning
- `TestGetRepoStatus`: Tests repository type detection
- `TestGetGitStatus`: Tests Git-specific status detection
- `TestGetJujutsuStatus`: Tests Jujutsu-specific status detection
- `TestGetDefaultDirectory`: Tests smart directory detection

### Writing Tests

- Place unit tests in `*_test.go` files
- Use temporary directories for filesystem operations
- Clean up resources with `defer` statements
- Tests require `git` and `jj` CLI tools to be installed

## Code Style

- Format code with `go fmt` (run `just format`)
- Follow Go idioms and best practices
- Keep functions focused and small
- Use meaningful variable and function names
- Avoid adding comments unless necessary for complex logic

## Git Hooks

This project uses [lefthook](https://github.com/evilmartians/lefthook) to run automatic checks:

- **Pre-commit**: Runs `just fix` to auto-fix issues, then `just check` to verify
- **Pre-push**: Runs `just check` to ensure code quality

To skip hooks temporarily:
```bash
git commit --no-verify
git push --no-verify
```

## Debugging

To debug issues:

1. Add debug output using `fmt.Fprintf(os.Stderr, ...)`
2. Use `go test -v` for verbose test output
3. Check command output with `cmd.CombinedOutput()`
4. Verify VCS commands manually in test directories