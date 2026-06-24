# AGENTS.md

This file provides guidance to Coding Agents when working with code in this repository.

---

## Coding Standard

- use `testify` for testing and assertions.
- use `golang-...` skills to follow Go best practices and idioms.
- follow spf13/cobra's best practices.

## Commands

You must check `mise run fmt`, `mise run lint:fix`, and `mise run test` before ending your work.

```bash
mise run test               # Run tests with coverage
mise run lint               # Run golangci-lint for code quality checks
mise run lint:fix           # Run golangci-lint and Auto-fix linting issues
mise run fmt                # Format code
mise run clean              # Remove generated files

# Standard Go commands
go test ./...               # Run all tests
go mod tidy                 # Clean up dependencies
```
