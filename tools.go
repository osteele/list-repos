//go:build tools
// +build tools

// Package main imports development tools to track them in go.mod.
// This ensures specific versions are pinned. To install: run `just setup`
// or manually: go install github.com/evilmartians/lefthook && go install github.com/golangci/golangci-lint/cmd/golangci-lint
package main

import (
	_ "github.com/evilmartians/lefthook"
	_ "github.com/golangci/golangci-lint/cmd/golangci-lint"
)