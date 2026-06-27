package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sushichan044/mergeable-please/internal/core/reviewer"
	"github.com/sushichan044/mergeable-please/internal/output"
)

func newCheckCmd(d deps, formatFlag *string) *cobra.Command {
	return &cobra.Command{
		Use:   "check [pr]",
		Short: "Check merge readiness of a pull request",
		Long: `Check whether a pull request is mergeable and show any blockers or advisories.

[pr] is optional. Accepted forms:
  - omitted: uses the current branch's open PR
  - bare number (e.g. 42): uses the current repo's PR #42
  - GitHub URL (e.g. https://github.com/owner/repo/pull/42)

Exit codes:
  0  PR is mergeable (all blockers resolved, reviewer loop terminal if configured)
  1  PR has unresolved blockers
  2  Usage, configuration, or API error`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolveFormat := makeFormatResolver(formatFlag)
			return runCheck(cmd.Context(), d, resolveFormat, args)
		},
	}
}

func runCheck(ctx context.Context, d deps, resolveFormat formatResolver, args []string) error {
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

	result, err := d.bundledEvaluate(ctx, pr)
	if err != nil {
		return fmt.Errorf("could not evaluate PR: %w", err)
	}

	// Attach reviewer loop only when reviewers are configured.
	policies, err := resolvePolicies(d)
	if err != nil {
		return err
	}

	var loopView *output.LoopView
	if len(policies) > 0 {
		snapshot, snapErr := d.fetchSnapshot(ctx, pr, policies)
		if snapErr != nil {
			return fmt.Errorf("could not fetch reviewer snapshot: %w", snapErr)
		}
		loopState := reviewer.EvaluateLoop(policies, snapshot)
		result.ReviewerLoop = &loopState

		comments, commentsErr := d.threadComments(ctx, pr, policies)
		if commentsErr != nil {
			return fmt.Errorf("could not fetch reviewer thread comments: %w", commentsErr)
		}
		lv := buildLoopView(loopState, snapshot, policies, comments)
		loopView = &lv
	}

	result.Finalize()

	if renderErr := output.RenderCheckResult(d.out, result, loopView, format); renderErr != nil {
		return renderErr
	}

	if !result.Satisfied {
		return ErrBlocked
	}
	return nil
}
