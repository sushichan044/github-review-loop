package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"

	"github.com/Songmu/skillsmith"

	"github.com/sushichan044/mergeable-please/cmd"
	"github.com/sushichan044/mergeable-please/internal/version"
)

// Process exit codes.
const (
	exitOK      = 0 // success / PR mergeable
	exitBlocked = 1 // PR is blocked (not yet mergeable) — an expected outcome
	exitError   = 2 // usage, configuration, or API error
)

// skillsFS embeds the agent skill shipped with the binary. skillsmith strips
// the leading "skills/" directory automatically.
//
//go:embed skills
var skillsFS embed.FS

func main() {
	os.Exit(run(os.Args[1:]))
}

// run dispatches the CLI and returns the process exit code.
func run(args []string) int {
	if len(args) > 0 && args[0] == "skills" {
		s, err := skillsmith.New("mergeable-please", version.Semver(), skillsFS)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitError
		}
		if runErr := s.Run(context.Background(), args[1:]); runErr != nil {
			fmt.Fprintln(os.Stderr, runErr)
			return exitError
		}
		return exitOK
	}

	if err := cmd.Execute(os.Stdout); err != nil {
		// ErrBlocked is an expected outcome: the full diagnosis (including the
		// status line) was already rendered to stdout, so don't print again.
		if errors.Is(err, cmd.ErrBlocked) {
			return exitBlocked
		}
		fmt.Fprintln(os.Stderr, err)
		return exitError
	}
	return exitOK
}
