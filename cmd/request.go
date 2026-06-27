package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sushichan044/mergeable-please/internal/backend/github"
	"github.com/sushichan044/mergeable-please/internal/core/reviewer"
)

func newRequestCmd(d deps) *cobra.Command {
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
Use --reviewer type:name to target a single reviewer.

Requires reviewers to be configured in .mergeable-please.yml.`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRequest(cmd.Context(), d, reviewerFlag, args)
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
	reviewerFlag string,
	args []string,
) error {
	policies, err := resolvePolicies(d)
	if err != nil {
		return err
	}
	if len(policies) == 0 {
		return errors.New("no reviewers configured in .mergeable-please.yml")
	}

	var prArg string
	if len(args) > 0 {
		prArg = args[0]
	}

	pr, err := resolvePR(ctx, prArg, d.resolver)
	if err != nil {
		return fmt.Errorf("could not resolve PR: %w", err)
	}

	snapshot, err := d.fetchSnapshot(ctx, pr, policies)
	if err != nil {
		return fmt.Errorf("could not fetch PR state: %w", err)
	}

	loopState := reviewer.EvaluateLoop(policies, snapshot)
	targets := selectTargets(loopState.Reviewers, policies, reviewerFlag)

	return fireRequests(d, pr, targets)
}

// reviewerTarget pairs a reviewer.State with its reviewer.Policy.
type reviewerTarget struct {
	state  reviewer.State
	policy reviewer.Policy
}

// selectTargets filters reviewer states to those that should be acted on.
func selectTargets(
	states []reviewer.State,
	policies []reviewer.Policy,
	reviewerFlag string,
) []reviewerTarget {
	policyByIdentity := make(map[reviewer.Identity]reviewer.Policy, len(policies))
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
func matchesFlag(id reviewer.Identity, flag string) bool {
	return strings.EqualFold(github.IdentityKey(id), flag)
}

// fireRequests iterates over targets, firing re-requests for eligible reviewers.
func fireRequests(d deps, pr github.PR, targets []reviewerTarget) error {
	for _, t := range targets {
		idStr := github.IdentityKey(t.state.Identity)

		if !t.state.CanRerequest {
			if _, err := fmt.Fprintf(d.out, "SKIP  %s — %s\n", idStr, t.state.BlockReason); err != nil {
				return err
			}
			continue
		}

		if err := d.triggerer.RequestReview(pr, t.policy); err != nil {
			return fmt.Errorf("failed to request review from %s: %w", idStr, err)
		}

		if _, err := fmt.Fprintf(d.out, "FIRED %s\n", idStr); err != nil {
			return err
		}
	}

	return nil
}
