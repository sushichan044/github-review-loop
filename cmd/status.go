package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sushichan044/github-review-loop/internal/config"
	"github.com/sushichan044/github-review-loop/internal/github"
	"github.com/sushichan044/github-review-loop/internal/output"
)

func newStatusCmd(d deps, resolveFormat formatResolver) *cobra.Command {
	return &cobra.Command{
		Use:   "status [pr]",
		Short: "Show the current review-loop status for a pull request",
		Long: `Show the current review-loop status for a pull request.

[pr] is optional. Accepted forms:
  - omitted: uses the current branch's open PR
  - bare number (e.g. 42): uses the current repo's PR #42
  - GitHub URL (e.g. https://github.com/owner/repo/pull/42)`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd.Context(), d, resolveFormat, args)
		},
	}
}

func runStatus(ctx context.Context, d deps, resolveFormat formatResolver, args []string) error {
	format, err := resolveFormat()
	if err != nil {
		return err
	}

	var prArg string
	if len(args) > 0 {
		prArg = args[0]
	}

	pr, err := resolvePR(ctx, prArg, d.resolver)
	if err != nil {
		return fmt.Errorf("could not resolve PR: %w", err)
	}

	cfg, err := d.loadConfig()
	if err != nil {
		return fmt.Errorf("could not load config: %w", err)
	}

	policies, err := config.Resolve(cfg, pr.Owner, pr.Repo)
	if err != nil {
		return fmt.Errorf("could not resolve policies: %w", err)
	}

	loopState, err := fetchEvaluate(ctx, pr, d, policies)
	if err != nil {
		return fmt.Errorf("could not fetch PR state: %w", err)
	}

	unresolvedByKey, err := github.UnresolvedThreadComments(ctx, d.client, pr, policies)
	if err != nil {
		return fmt.Errorf("could not fetch unresolved comments: %w", err)
	}

	view := buildLoopView(loopState, policies, unresolvedByKey)

	return output.Render(d.out, view, format)
}
