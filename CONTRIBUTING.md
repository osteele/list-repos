# Contributing to list-repos

Thank you for your interest in contributing to list-repos!

## How to Contribute

### Reporting Issues

- Use GitHub Issues to report bugs or suggest features
- Include clear steps to reproduce bugs
- Provide system information (OS, Go version)
- Include error messages and stack traces when applicable

### Submitting Pull Requests

1. Fork the repository
2. Create a new branch for your feature or fix
3. Make your changes
4. Run `just fix` to fix formatting and fixable lint errors
5. Run `just check` to verify linting and tests.
6. Update documentation if needed
7. Commit your changes with clear, descriptive messages
8. Push your branch and create a pull request

### Commit Messages

- Use clear, descriptive commit messages
- Start with a verb in present tense (e.g., "Add", "Fix", "Update")
- Keep the first line under 72 characters
- Reference issues when applicable (e.g., "Fix #123")

### Code Style

- Code is formatted with `go fmt`
- Follow Go best practices and idioms
- Add tests for new functionality
- Keep functions focused and small
- Use meaningful variable and function names

### Testing

- Write unit tests for new functionality
- Place unit tests in `*_test.go` files alongside the code they test
- Integration tests use the `integration` build tag
- Run tests with `just test` or `just test-coverage`

## Development Setup

For detailed development instructions, see [DEVELOPMENT.md](DEVELOPMENT.md).

## Code of Conduct

Please be respectful and considerate in all interactions. We welcome contributors of all backgrounds and experience levels.

## Questions?

Feel free to open an issue for any questions about contributing.
