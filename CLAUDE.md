# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Go module named `settlement` (Go 1.26). Currently a fresh scaffold — `main.go` contains only an empty `main()` function with no dependencies added yet.

## Commands

```bash
# Run
go run main.go

# Build
go build -o settlement .

# Test
go test ./...

# Single package test
go test ./path/to/pkg/...

# Lint (if golangci-lint is available)
golangci-lint run
```

## Conventions

- Follow standard Go project layout as the codebase grows (`cmd/`, `internal/`, `pkg/` as appropriate)
- Use `go mod tidy` after adding or removing dependencies
