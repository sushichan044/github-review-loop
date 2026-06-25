// Package cmd implements the github-review-loop CLI commands.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/sushichan044/github-review-loop/internal/config"
	"github.com/sushichan044/github-review-loop/internal/github"
	"github.com/sushichan044/github-review-loop/internal/output"
	"github.com/sushichan044/github-review-loop/internal/reviewloop"
)

// configLoader abstracts config.Load for testability.
type configLoader func() (*config.Config, error)

// configInitializer abstracts config.Init for testability. It returns the path
// of the written config file.
type configInitializer func() (string, error)

// deps holds all injected dependencies for the CLI commands.
// Tests substitute fakes; production uses the real implementations.
type deps struct {
	resolver       github.PRResolver
	fetchSnapshot  func(ctx context.Context, pr github.PR, policies []reviewloop.Policy) (reviewloop.Snapshot, error)
	threadComments func(ctx context.Context, pr github.PR, policies []reviewloop.Policy) (map[string][]github.ThreadComment, error)
	triggerer      *github.Triggerer
	loadConfig     configLoader
	initConfig     configInitializer
	out            io.Writer
}

// noConfigHint is printed when a command runs in a repository that has no
// review-loop config yet. It points the user at `init` instead of failing.
const noConfigHint = "No review-loop config found. Run `github-review-loop init` to create .github/review-loop.yml."

// formatResolver returns the [output.Format] to use, resolving the --format flag.
type formatResolver func() (output.Format, error)

// newRootCmd constructs the root cobra command with all subcommands wired.
// All dependencies are injected via d; no package-level state is used.
func newRootCmd(d deps) *cobra.Command {
	var formatFlag string

	root := &cobra.Command{
		Use:   "github-review-loop",
		Short: "Manage AI code-review loops on GitHub pull requests",
		Long: `github-review-loop tracks the state of AI reviewer loops on GitHub pull requests.

It stateless-ly reconstructs rally counts, goal status, and unresolved threads
from the PR event history, and fires review re-requests when appropriate.`,
		SilenceUsage: true,
	}

	root.PersistentFlags().StringVar(
		&formatFlag, "format", "",
		`output format: "human" or "agent" (default: auto-detect via agentdetection)`,
	)

	resolveFormat := formatResolver(func() (output.Format, error) {
		if formatFlag == "" {
			return output.DefaultFormat(), nil
		}

		return output.ParseFormat(formatFlag)
	})

	root.AddCommand(newStatusCmd(d, resolveFormat))
	root.AddCommand(newRequestCmd(d, resolveFormat))
	root.AddCommand(newInitCmd(d))

	return root
}

// Execute builds the production dependency set, constructs the root command,
// and runs it. Errors are returned to main for exit-code handling.
func Execute(w io.Writer) error {
	client, err := github.NewClient()
	if err != nil {
		return err
	}

	d := deps{
		resolver: github.GHPRResolver{},
		fetchSnapshot: func(ctx context.Context, pr github.PR, policies []reviewloop.Policy) (reviewloop.Snapshot, error) {
			return github.FetchSnapshot(ctx, client, pr, policies)
		},
		threadComments: func(ctx context.Context, pr github.PR, policies []reviewloop.Policy) (map[string][]github.ThreadComment, error) {
			return github.ThreadComments(ctx, client, pr, policies)
		},
		triggerer:  github.NewTriggerer(),
		loadConfig: config.Load,
		initConfig: config.Init,
		out:        w,
	}

	return newRootCmd(d).Execute()
}

// resolvePR resolves the target PR from an optional positional argument.
//
// Resolution order:
//  1. If arg is non-empty: parse as number or GitHub URL via [github.ParsePRArg].
//     For bare numbers, owner/repo come from [github.PRResolver.CurrentPR].
//  2. If arg is empty: delegate to [github.PRResolver.CurrentPR].
func resolvePR(
	ctx context.Context,
	arg string,
	resolver github.PRResolver,
) (github.PR, error) {
	if arg != "" {
		owner, repo, number, err := github.ParsePRArg(arg)
		if err != nil {
			return github.PR{}, err
		}

		if owner == "" || repo == "" {
			// Bare number: fill owner/repo from current repo context.
			o, r, _, resolveErr := resolver.CurrentPR(ctx)
			if resolveErr != nil {
				return github.PR{}, resolveErr
			}

			owner, repo = o, r
		}

		return github.PR{Owner: owner, Repo: repo, Number: number}, nil
	}

	owner, repo, number, err := resolver.CurrentPR(ctx)
	if err != nil {
		return github.PR{}, err
	}

	return github.PR{Owner: owner, Repo: repo, Number: number}, nil
}

// resolvePolicies loads the repository's config and maps it to policies. When
// the repo has no config yet it prints the init hint and returns ok=false, so
// the caller stops without treating it as an error. It is called before PR
// resolution (a network call) so an unconfigured repo gets the hint rather
// than a confusing PR-lookup failure.
func resolvePolicies(d deps) ([]reviewloop.Policy, bool, error) {
	cfg, err := d.loadConfig()
	if errors.Is(err, config.ErrConfigNotFound) {
		if _, printErr := fmt.Fprintln(d.out, noConfigHint); printErr != nil {
			return nil, false, printErr
		}
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("could not load config: %w", err)
	}

	policies, err := config.Policies(cfg)
	if err != nil {
		return nil, false, fmt.Errorf("could not resolve policies: %w", err)
	}

	if len(policies) == 0 {
		return nil, false, errors.New("no reviewers configured in .github/review-loop.yml")
	}

	return policies, true, nil
}

// fetchEvaluate is the shared fetch+evaluate pipeline used by both status and request.
// It returns the raw Snapshot alongside the evaluated LoopState so callers can
// access trigger timing for features like NewComments attribution.
func fetchEvaluate(
	ctx context.Context,
	pr github.PR,
	d deps,
	policies []reviewloop.Policy,
) (reviewloop.LoopState, reviewloop.Snapshot, error) {
	snapshot, err := d.fetchSnapshot(ctx, pr, policies)
	if err != nil {
		return reviewloop.LoopState{}, reviewloop.Snapshot{}, err
	}

	return reviewloop.EvaluateLoop(policies, snapshot), snapshot, nil
}
