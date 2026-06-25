package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sushichan044/github-review-loop/internal/github"
	"github.com/sushichan044/github-review-loop/internal/reviewloop"
)

func newRequestCmd(d deps, resolveFormat formatResolver) *cobra.Command {
	var reviewerFlag string

	cmd := &cobra.Command{
		Use:   "request [pr]",
		Short: "Fire review re-requests for eligible reviewers",
		Long: `Fire review re-requests for eligible reviewers on a pull request.

[pr] is optional. Accepted forms:
  - omitted: uses the current branch's open PR
  - bare number (e.g. 42): uses the current repo's PR #42
  - GitHub URL (e.g. https://github.com/owner/repo/pull/42)

By default, all reviewers with CanRerequest == true are targeted.
Use --reviewer type:name to target a single reviewer.`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRequest(cmd.Context(), d, resolveFormat, reviewerFlag, args)
		},
	}

	cmd.Flags().StringVar(
		&reviewerFlag, "reviewer", "",
		`target a single reviewer by identity, e.g. "user:alice" or "github-copilot"`,
	)

	return cmd
}

func runRequest(
	ctx context.Context,
	d deps,
	resolveFormat formatResolver,
	reviewerFlag string,
	args []string,
) error {
	// Validate the --format flag eagerly so users get immediate feedback on bad values.
	if _, err := resolveFormat(); err != nil {
		return err
	}

	policies, ok, err := resolvePolicies(d)
	if err != nil || !ok {
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

	loopState, _, err := fetchEvaluate(ctx, pr, d, policies)
	if err != nil {
		return fmt.Errorf("could not fetch PR state: %w", err)
	}

	targets := selectTargets(loopState.Reviewers, policies, reviewerFlag)

	return fireRequests(d, pr, policies, targets)
}

// reviewerTarget pairs a [reviewloop.ReviewerState] with its [reviewloop.Policy].
type reviewerTarget struct {
	state  reviewloop.ReviewerState
	policy reviewloop.Policy
}

// selectTargets filters the reviewer states to those that should be acted upon.
// When reviewerFlag is non-empty, only the reviewer with that identity is included.
// Otherwise, all reviewers are included (both eligible and blocked, so the caller
// can print appropriate no-op messages).
func selectTargets(
	states []reviewloop.ReviewerState,
	policies []reviewloop.Policy,
	reviewerFlag string,
) []reviewerTarget {
	policyByIdentity := make(map[reviewloop.ReviewerIdentity]reviewloop.Policy, len(policies))
	for _, p := range policies {
		policyByIdentity[p.Identity] = p
	}

	var targets []reviewerTarget

	for _, rs := range states {
		if reviewerFlag != "" && !matchesFlag(rs.Identity, reviewerFlag) {
			continue
		}

		targets = append(targets, reviewerTarget{
			state:  rs,
			policy: policyByIdentity[rs.Identity],
		})
	}

	return targets
}

// matchesFlag reports whether the identity matches the "type:name" (or "type") flag value.
func matchesFlag(id reviewloop.ReviewerIdentity, flag string) bool {
	return strings.EqualFold(identityKeyFromReviewerIdentity(id), flag)
}

// fireRequests iterates over targets, firing re-requests for eligible reviewers
// and printing no-op messages for blocked ones.
func fireRequests(
	d deps,
	pr github.PR,
	_ []reviewloop.Policy,
	targets []reviewerTarget,
) error {
	for _, t := range targets {
		idStr := identityKeyFromReviewerIdentity(t.state.Identity)

		if !t.state.CanRerequest {
			_, err := fmt.Fprintf(
				d.out,
				"SKIP  %s — %s\n",
				idStr,
				t.state.BlockReason,
			)
			if err != nil {
				return err
			}

			continue
		}

		if err := d.triggerer.RequestReview(pr, t.policy); err != nil {
			return fmt.Errorf("failed to request review from %s: %w", idStr, err)
		}

		_, err := fmt.Fprintf(d.out, "FIRED %s\n", idStr)
		if err != nil {
			return err
		}
	}

	return nil
}
