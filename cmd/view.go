package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sushichan044/mergeable-please/internal/backend/github"
	"github.com/sushichan044/mergeable-please/internal/core/reviewer"
	"github.com/sushichan044/mergeable-please/internal/output"
)

func newViewCmd(d deps, formatFlag *string) *cobra.Command {
	var conditionFlag string

	cmd := &cobra.Command{
		Use:   "view [pr]",
		Short: "Show detailed information for a specific merge condition",
		Long: `Show detailed information for a specific merge-readiness condition.

[pr] is optional. Accepted forms:
  - omitted: uses the current branch's open PR
  - bare number (e.g. 42): uses the current repo's PR #42
  - GitHub URL (e.g. https://github.com/owner/repo/pull/42)

--condition choices:
  checks     Show failing/pending required CI checks
  conflicts  Show conflict status
  rules      Show configured branch ruleset rules (REST API call)
  reviewers  Show reviewer loop state and thread comments`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolveFormat := makeFormatResolver(formatFlag)
			return runView(cmd.Context(), d, resolveFormat, conditionFlag, args)
		},
	}

	cmd.Flags().StringVar(
		&conditionFlag, "condition", "",
		`condition to drill into: "conflicts", "checks", "rules", or "reviewers"`,
	)

	return cmd
}

func runView(ctx context.Context, d deps, resolveFormat formatResolver, condition string, args []string) error {
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

	switch condition {
	case "rules":
		return runViewRules(ctx, d, pr, format)
	case "reviewers":
		return runViewReviewers(ctx, d, pr, format)
	case "checks", "conflicts", "":
		// Re-run BundledEvaluate and show the full result; the check output
		// already contains detailed info for checks/conflicts.
		result, evalErr := d.bundledEvaluate(ctx, pr)
		if evalErr != nil {
			return fmt.Errorf("could not evaluate PR: %w", evalErr)
		}
		result.Finalize()
		return output.RenderCheckResult(d.out, result, format)
	default:
		return fmt.Errorf("unknown --condition %q: must be conflicts, checks, rules, or reviewers", condition)
	}
}

func runViewRules(ctx context.Context, d deps, pr github.PR, _ output.Format) error {
	if d.fetchBranchRules == nil {
		_, err := fmt.Fprintln(d.out, "Branch rules are not available in this configuration.")
		return err
	}

	rules, err := d.fetchBranchRules(ctx, pr)
	if err != nil {
		return fmt.Errorf("could not fetch branch rules: %w", err)
	}

	if len(rules) == 0 {
		_, err = fmt.Fprintln(d.out, "No branch rules configured.")
		return err
	}

	_, err = fmt.Fprintf(d.out, "Branch rules (%d):\n", len(rules))
	if err != nil {
		return err
	}

	for _, r := range rules {
		if _, err = fmt.Fprintf(d.out, "  - type: %s\n", r.Type); err != nil {
			return err
		}
	}
	return nil
}

func runViewReviewers(ctx context.Context, d deps, pr github.PR, format output.Format) error {
	policies, err := resolvePolicies(d)
	if err != nil {
		return err
	}

	if len(policies) == 0 {
		_, err = fmt.Fprintln(d.out,
			"No reviewers configured. Add reviewers to .mergeable-please.yml to enable the reviewer loop.")
		return err
	}

	snapshot, err := d.fetchSnapshot(ctx, pr, policies)
	if err != nil {
		return fmt.Errorf("could not fetch reviewer snapshot: %w", err)
	}

	allCommentsByKey, err := d.threadComments(ctx, pr, policies)
	if err != nil {
		return fmt.Errorf("could not fetch thread comments: %w", err)
	}

	loopState := reviewer.EvaluateLoop(policies, snapshot)
	view := buildLoopView(loopState, snapshot, policies, allCommentsByKey)

	return output.Render(d.out, view, format)
}

// buildLoopView maps a reviewer.LoopState into an output.LoopView.
func buildLoopView(
	state reviewer.LoopState,
	snapshot reviewer.Snapshot,
	policies []reviewer.Policy,
	allCommentsByKey map[string][]github.ThreadComment,
) output.LoopView {
	policyByIdentity := make(map[reviewer.Identity]reviewer.Policy, len(policies))
	for _, p := range policies {
		policyByIdentity[p.Identity] = p
	}

	reviewerViews := make([]output.ReviewerView, 0, len(state.Reviewers))

	for _, rs := range state.Reviewers {
		p := policyByIdentity[rs.Identity]
		key := identityKeyFromReviewerIdentity(rs.Identity)
		lastRally := lastRallyTime(rs.Identity, snapshot.Triggers)

		var unresolvedComments []output.CommentView
		var newComments []output.CommentView

		for _, c := range allCommentsByKey[key] {
			cv := output.CommentView{
				Author: c.Author,
				Body:   c.Body,
				URL:    c.URL,
				At:     c.CreatedAt,
			}
			if !c.Resolved {
				unresolvedComments = append(unresolvedComments, cv)
			}
			if c.CreatedAt.After(lastRally) {
				newComments = append(newComments, cv)
			}
		}

		reviewerViews = append(reviewerViews, output.ReviewerView{
			Identity:           rs.Identity,
			Goal:               p.Goal,
			Phase:              rs.Phase,
			RallyCount:         rs.RallyCount,
			MaxRallies:         p.MaxRallies,
			GoalMet:            rs.GoalMet,
			CanRerequest:       rs.CanRerequest,
			BlockReason:        rs.BlockReason,
			UnresolvedComments: unresolvedComments,
			NewComments:        newComments,
		})
	}

	return output.LoopView{
		Reviewers: reviewerViews,
		Done:      state.Done,
	}
}

// lastRallyTime returns the latest TriggerAction.At for the given identity.
func lastRallyTime(identity reviewer.Identity, triggers []reviewer.TriggerAction) time.Time {
	var latest time.Time
	for _, t := range triggers {
		if t.Reviewer.Type != identity.Type {
			continue
		}
		if !strings.EqualFold(t.Reviewer.Name, identity.Name) {
			continue
		}
		if t.At.After(latest) {
			latest = t.At
		}
	}
	return latest
}

// identityKeyFromReviewerIdentity returns the canonical "type:name" key string.
func identityKeyFromReviewerIdentity(id reviewer.Identity) string {
	if id.Name == "" {
		return string(id.Type)
	}
	return string(id.Type) + ":" + id.Name
}
