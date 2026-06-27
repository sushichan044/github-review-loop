package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sushichan044/mergeable-please/internal/backend/github"
	"github.com/sushichan044/mergeable-please/internal/core"
	"github.com/sushichan044/mergeable-please/internal/core/reviewer"
	"github.com/sushichan044/mergeable-please/internal/output"
)

func newViewCmd(d deps) *cobra.Command {
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
			return runView(cmd.Context(), d, conditionFlag, args)
		},
	}

	cmd.Flags().StringVar(
		&conditionFlag, "condition", "",
		`condition to drill into: "conflicts", "checks", "rules", or "reviewers"`,
	)

	return cmd
}

func runView(ctx context.Context, d deps, condition string, args []string) error {
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
		return runViewRules(ctx, d, pr)
	case "reviewers":
		return runViewReviewers(ctx, d, pr)
	case "checks":
		// Show only check-related conditions; omit global status (reviewer loop not evaluated).
		result, evalErr := d.bundledEvaluate(ctx, pr)
		if evalErr != nil {
			return fmt.Errorf("could not evaluate PR: %w", evalErr)
		}
		blockers := filterConditionsByKind(result.Blockers, core.ConditionCheckFailing, core.ConditionCheckPending)
		return output.RenderDimensionView(d.out, blockers, nil)
	case "conflicts":
		// Show only conflict-related conditions; omit global status.
		result, evalErr := d.bundledEvaluate(ctx, pr)
		if evalErr != nil {
			return fmt.Errorf("could not evaluate PR: %w", evalErr)
		}
		blockers := filterConditionsByKind(result.Blockers, core.ConditionConflict, core.ConditionBehindBase)
		return output.RenderDimensionView(d.out, blockers, nil)
	case "":
		// Full dimension view: all conditions, but no global status verdict.
		result, evalErr := d.bundledEvaluate(ctx, pr)
		if evalErr != nil {
			return fmt.Errorf("could not evaluate PR: %w", evalErr)
		}
		return output.RenderDimensionView(d.out, result.Blockers, result.Advisories)
	default:
		return fmt.Errorf("unknown --condition %q: must be conflicts, checks, rules, or reviewers", condition)
	}
}

func runViewRules(ctx context.Context, d deps, pr github.PR) error {
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

func runViewReviewers(ctx context.Context, d deps, pr github.PR) error {
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
	view := buildLoopView(loopState, policies, allCommentsByKey)

	return output.Render(d.out, view)
}

// buildLoopView maps a reviewer.LoopState into an output.LoopView.
func buildLoopView(
	state reviewer.LoopState,
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
		key := github.IdentityKey(rs.Identity)

		var unresolvedComments []output.CommentView
		for _, c := range allCommentsByKey[key] {
			if c.Resolved {
				continue
			}
			unresolvedComments = append(unresolvedComments, output.CommentView{
				Author: c.Author,
				Body:   c.Body,
				URL:    c.URL,
				At:     c.CreatedAt,
			})
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
		})
	}

	return output.LoopView{
		Reviewers: reviewerViews,
		Done:      state.Done,
	}
}

// filterConditionsByKind returns only those conditions whose Kind is in the allow-list.
func filterConditionsByKind(conditions []core.Condition, kinds ...core.ConditionKind) []core.Condition {
	allow := make(map[core.ConditionKind]bool, len(kinds))
	for _, k := range kinds {
		allow[k] = true
	}
	filtered := make([]core.Condition, 0, len(conditions))
	for _, c := range conditions {
		if allow[c.Kind] {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// buildConciseLoopView builds a LoopView for the check path.
// It uses only snapshot.Threads to count unresolved comments per reviewer —
// no ThreadComments round-trip is needed, keeping check fast and output lean.
func buildConciseLoopView(
	state reviewer.LoopState,
	snapshot reviewer.Snapshot,
	policies []reviewer.Policy,
	pr github.PR,
) output.LoopView {
	// Count unresolved threads per identity key from the already-fetched snapshot.
	unresolvedCounts := make(map[string]int)
	for _, t := range snapshot.Threads {
		if !t.Resolved {
			key := github.IdentityKey(t.Reviewer)
			unresolvedCounts[key]++
		}
	}

	policyByIdentity := make(map[reviewer.Identity]reviewer.Policy, len(policies))
	for _, p := range policies {
		policyByIdentity[p.Identity] = p
	}

	reviewerViews := make([]output.ReviewerView, 0, len(state.Reviewers))
	for _, rs := range state.Reviewers {
		p := policyByIdentity[rs.Identity]
		key := github.IdentityKey(rs.Identity)

		reviewerViews = append(reviewerViews, output.ReviewerView{
			Identity:        rs.Identity,
			Goal:            p.Goal,
			Phase:           rs.Phase,
			RallyCount:      rs.RallyCount,
			MaxRallies:      p.MaxRallies,
			GoalMet:         rs.GoalMet,
			CanRerequest:    rs.CanRerequest,
			BlockReason:     rs.BlockReason,
			UnresolvedCount: unresolvedCounts[key],
			DrillInCmd:      reviewerCommentsDrillIn(rs.Identity, pr),
		})
	}

	return output.LoopView{
		Reviewers: reviewerViews,
		Done:      state.Done,
	}
}

// reviewerCommentsDrillIn returns a gh command that reads a reviewer's UNRESOLVED
// review-thread comments via the GraphQL API. REST review comments carry no
// resolved state, so GraphQL reviewThreads.isResolved is used to filter.
func reviewerCommentsDrillIn(id reviewer.Identity, pr github.PR) string {
	// Config allows arbitrary reviewer names, so the login is untrusted at two
	// layers. strconv.Quote makes it a valid jq (JSON) string literal — so a name
	// containing " or \ cannot break the jq expression — and the whole jq program
	// is then shell-single-quote-escaped so a name containing ' cannot break out
	// of the single-quoted --jq argument.
	loginLiteral := strconv.Quote(strings.ToLower(reviewerCommentLogin(id)))
	const query = "query($o:String!,$r:String!,$n:Int!){repository(owner:$o,name:$r)" +
		"{pullRequest(number:$n){reviewThreads(first:100){nodes{isResolved " +
		"comments(first:1){nodes{author{login} body path line url}}}}}}}"
	jq := fmt.Sprintf(
		"[.data.repository.pullRequest.reviewThreads.nodes[] | select(.isResolved==false) "+
			"| .comments.nodes[] | select(.author.login|ascii_downcase|startswith(%s)) "+
			"| {path,line,body,url}]",
		loginLiteral,
	)
	return fmt.Sprintf(
		"gh api graphql -f query='%s' -f o=%s -f r=%s -F n=%d --jq '%s'",
		query, pr.Owner, pr.Repo, pr.Number, shellSingleQuoteEscape(jq),
	)
}

// shellSingleQuoteEscape makes s safe to embed inside a single-quoted shell
// argument by replacing each single quote with the close/escape/reopen sequence
// '\” (close the quote, an escaped single quote, reopen the quote).
func shellSingleQuoteEscape(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}

// reviewerCommentLogin returns the expected GitHub comment-author login prefix for a reviewer.
func reviewerCommentLogin(id reviewer.Identity) string {
	switch id.Type {
	case reviewer.ReviewerTypeGitHubCopilot:
		// Copilot reviews appear under "copilot-pull-request-reviewer"; "copilot" is a safe prefix.
		return "copilot"
	case reviewer.ReviewerTypeUser, reviewer.ReviewerTypeGitHubApp:
		return id.Name
	}
	return id.Name
}
