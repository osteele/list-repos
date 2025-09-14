# Default target - show available commands
default:
    @just --list

# Install dependencies and development tools
setup:
    @echo "Downloading project dependencies..."
    go mod download
    go mod tidy
    @echo "Installing development tools..."
    go install github.com/evilmartians/lefthook
    go install github.com/golangci/golangci-lint/cmd/golangci-lint
    @echo "Setting up git hooks..."
    lefthook install
    @echo "Setup complete!"

# Run all quality checks
check: format lint test

# Check code formatting (without fixing)
format:
    @! gofmt -l . | grep . || (echo "All files are properly formatted"; exit 0)

# Run linting checks
lint:
    go vet ./...
    golangci-lint run

# Auto-fix linting and formatting issues
fix:
    go fmt ./...
    golangci-lint run --fix

# Run tests
test:
    go test -v ./...

# Run tests with coverage
test-coverage:
    go test -v -cover ./...

# Run integration tests
integration-test:
    go test -v -tags=integration ./...

# Build the project
build:
    go build -o list-repos .

# Run the CLI application
run *ARGS:
    go run . {{ARGS}}

# Clean build artifacts
clean:
    rm -f list-repos dir-status
    go clean

# Update dependencies
update:
    go get -u ./...
    go mod tidy
