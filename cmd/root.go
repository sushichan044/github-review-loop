// Package cmd implements the mergeable-please CLI commands.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/sushichan044/mergeable-please/internal/backend"
	"github.com/sushichan044/mergeable-please/internal/backend/github"
	"github.com/sushichan044/mergeable-please/internal/config"
	"github.com/sushichan044/mergeable-please/internal/core"
	"github.com/sushichan044/mergeable-please/internal/core/reviewer"
)

// ErrBlocked is returned by the check command when the PR is not yet mergeable.
// main() translates this to exit code 1 (blocked), distinct from exit 2 (error).
var ErrBlocked = errors.New("PR is not yet mergeable")

// configLoader abstracts config.Load for testability.
type configLoader func() (*config.Config, error)

// configInitializer abstracts config.Init for testability.
type configInitializer func() (string, error)

// deps holds all injected dependencies for the CLI commands.
// Tests substitute fakes; production wires the real implementations.
type deps struct {
	resolver         github.PRResolver
	bundledEvaluate  func(ctx context.Context, pr github.PR) (core.CheckResult, error)
	fetchSnapshot    func(ctx context.Context, pr github.PR, policies []reviewer.Policy) (reviewer.Snapshot, error)
	threadComments   func(ctx context.Context, pr github.PR, policies []reviewer.Policy) (map[string][]github.ThreadComment, error)
	fetchBranchRules func(ctx context.Context, pr github.PR) ([]backend.BranchRule, error)
	triggerer        *github.Triggerer
	loadConfig       configLoader
	initConfig       configInitializer
	out              io.Writer
}

// newRootCmd constructs the root cobra command with all subcommands wired.
func newRootCmd(d deps) *cobra.Command {
	root := &cobra.Command{
		Use:   "mergeable-please",
		Short: "Check and advance pull request merge readiness",
		Long: `mergeable-please checks whether a GitHub pull request is mergeable and,
when configured, loops AI reviewer interactions until all goals are met.

Running without a subcommand is equivalent to running 'check'.

No configuration file is required: the default settings check for conflicts,
required CI failures, and ruleset blockers. Reviewer loops are opt-in via
.mergeable-please.yml at the repository root.`,
		SilenceUsage: true,
		// main() owns error printing and exit-code mapping; don't let cobra
		// also print the error (which would duplicate the message).
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCheck(cmd.Context(), d, args)
		},
	}

	root.AddCommand(newCheckCmd(d))
	root.AddCommand(newRequestCmd(d))
	root.AddCommand(newViewCmd(d))
	root.AddCommand(newInitCmd(d))

	return root
}

// Execute builds the production dependency set, constructs the root command,
// and runs it. Errors are returned to main for exit-code handling.
func Execute(w io.Writer) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("could not load config: %w", err)
	}

	client, err := github.NewClient()
	if err != nil {
		return err
	}
	be := github.NewGitHubBackendWithClient(client)

	d := deps{
		resolver: github.GHPRResolver{},
		bundledEvaluate: func(ctx context.Context, pr github.PR) (core.CheckResult, error) {
			return be.BundledEvaluate(ctx, backend.PRCoords{Owner: pr.Owner, Repo: pr.Repo, Number: pr.Number})
		},
		fetchSnapshot: func(ctx context.Context, pr github.PR, policies []reviewer.Policy) (reviewer.Snapshot, error) {
			return github.FetchSnapshot(ctx, client, pr, policies)
		},
		threadComments: func(ctx context.Context, pr github.PR, policies []reviewer.Policy) (map[string][]github.ThreadComment, error) {
			return github.ThreadComments(ctx, client, pr, policies)
		},
		fetchBranchRules: func(ctx context.Context, pr github.PR) ([]backend.BranchRule, error) {
			return be.FetchBranchRules(ctx, backend.PRCoords{Owner: pr.Owner, Repo: pr.Repo, Number: pr.Number})
		},
		triggerer:  github.NewTriggerer(),
		loadConfig: func() (*config.Config, error) { return cfg, nil },
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
func resolvePR(ctx context.Context, arg string, resolver github.PRResolver) (github.PR, error) {
	if arg != "" {
		owner, repo, number, err := github.ParsePRArg(arg)
		if err != nil {
			return github.PR{}, err
		}

		if owner == "" || repo == "" {
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

// resolvePolicies extracts reviewer policies from the loaded config.
// Returns empty policies when no reviewers are configured (not an error — the
// check command works config-less; request validates non-empty separately).
func resolvePolicies(d deps) ([]reviewer.Policy, error) {
	cfg, err := d.loadConfig()
	if err != nil {
		return nil, fmt.Errorf("could not load config: %w", err)
	}

	policies, err := config.Policies(cfg)
	if err != nil {
		return nil, fmt.Errorf("could not resolve reviewer policies: %w", err)
	}

	return policies, nil
}
