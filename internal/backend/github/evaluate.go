package github

import (
	"context"
	"fmt"
	"strings"
	"time"

	graphql "github.com/cli/shurcooL-graphql"

	"github.com/sushichan044/mergeable-please/internal/backend"
	"github.com/sushichan044/mergeable-please/internal/core"
)

const (
	defaultRetryCount = 3
	defaultRetryDelay = 2 * time.Second

	// gqlVarOwner/Repo/Number are the GraphQL variable names used in every query.
	gqlVarOwner  = "owner"
	gqlVarRepo   = "repo"
	gqlVarNumber = "number"

	mergeableUnknown = "UNKNOWN"
)

// checkContextNode is one item in statusCheckRollup.contexts — a union of
// CheckRun and StatusContext. Both sub-structs are always present; only the
// one matching the actual __typename will be populated by the GraphQL client.
//
// RISK: the field argument `isRequired(pullRequestNumber: $number)` references
// the outer query variable $number. This is valid GraphQL but depends on
// shurcooL-graphql propagating inline-fragment field-arg variable references
// to the operation-level variable declarations. Verified to compile; live-API
// correctness needs manual PR testing (see plan §検証 item 1).
type checkContextNode struct {
	CheckRun struct {
		Name       string `graphql:"name"`
		Status     string `graphql:"status"`
		Conclusion string `graphql:"conclusion"`
		DetailsURL string `graphql:"detailsUrl"`
		IsRequired bool   `graphql:"isRequired(pullRequestNumber: $number)"`
	} `graphql:"... on CheckRun"`
	StatusContext struct {
		Context    string `graphql:"context"`
		State      string `graphql:"state"`
		TargetURL  string `graphql:"targetUrl"`
		IsRequired bool   `graphql:"isRequired(pullRequestNumber: $number)"`
	} `graphql:"... on StatusContext"`
}

// prMergeabilityQueryStruct is the shurcooL-graphql struct for the single
// batched merge-readiness query used by BundledEvaluate.
type prMergeabilityQueryStruct struct {
	Repository struct {
		PullRequest struct {
			Mergeable         string `graphql:"mergeable"`
			MergeStateStatus  string `graphql:"mergeStateStatus"`
			ReviewDecision    string `graphql:"reviewDecision"`
			HeadRefOid        string `graphql:"headRefOid"`
			BaseRefName       string `graphql:"baseRefName"`
			StatusCheckRollup struct {
				Contexts struct {
					Nodes    []checkContextNode
					PageInfo struct {
						HasNextPage bool
					}
				} `graphql:"contexts(first: 100)"`
			} `graphql:"statusCheckRollup"`
		} `graphql:"pullRequest(number: $number)"`
	} `graphql:"repository(owner: $owner, name: $repo)"`
}

// GitHubBackend implements [backend.Backend] for GitHub using go-gh.
//
//nolint:revive // "GitHubBackend" stutter is intentional: the package is named github.
type GitHubBackend struct {
	client       *Client
	retrySleeper func(time.Duration)
	retryCount   int
}

// backendOption configures a GitHubBackend.
type backendOption func(*GitHubBackend)

// withRetrySleeper injects a custom sleep function used between UNKNOWN retries.
// The default is [time.Sleep]. Use a no-op for tests.
func withRetrySleeper(fn func(time.Duration)) backendOption {
	return func(b *GitHubBackend) { b.retrySleeper = fn }
}

// withRetryCount sets the maximum number of sleeps between UNKNOWN retries.
// The default is defaultRetryCount (3), meaning up to 4 queries total.
func withRetryCount(n int) backendOption {
	return func(b *GitHubBackend) { b.retryCount = n }
}

// newBackend creates a GitHubBackend using the given client.
func newBackend(client *Client, opts ...backendOption) *GitHubBackend {
	b := &GitHubBackend{
		client:       client,
		retrySleeper: time.Sleep,
		retryCount:   defaultRetryCount,
	}
	for _, o := range opts {
		o(b)
	}
	return b
}

// NewGitHubBackendWithClient creates a GitHubBackend using an already-constructed Client.
// Use this when the caller has already created a Client for other purposes (e.g., FetchSnapshot).
func NewGitHubBackendWithClient(client *Client) *GitHubBackend {
	return newBackend(client)
}

// BundledEvaluate executes one batched GraphQL query and attributes Blockers
// and Advisories from the result. It retries up to retryCount times when
// mergeable/mergeStateStatus is UNKNOWN (GitHub is still computing).
//
// The returned CheckResult does NOT have Finalize called; callers must call
// CheckResult.Finalize after optionally attaching ReviewerLoop.
func (b *GitHubBackend) BundledEvaluate(_ context.Context, pr backend.PRCoords) (core.CheckResult, error) {
	vars := map[string]any{
		gqlVarOwner:  graphql.String(pr.Owner),
		gqlVarRepo:   graphql.String(pr.Repo),
		gqlVarNumber: graphql.Int(int32(pr.Number)), //nolint:gosec // PR numbers won't overflow int32
	}

	var q prMergeabilityQueryStruct
	for attempt := 0; ; attempt++ {
		if err := b.client.gql.Query("PRMergeability", &q, vars); err != nil {
			return core.CheckResult{}, fmt.Errorf("mergeability query failed: %w", err)
		}

		mergeable := q.Repository.PullRequest.Mergeable
		mergeState := q.Repository.PullRequest.MergeStateStatus

		if (mergeable != mergeableUnknown && mergeState != mergeableUnknown) || attempt >= b.retryCount {
			break
		}
		b.retrySleeper(defaultRetryDelay)
	}

	var result core.CheckResult
	result.HeadCommitOID = q.Repository.PullRequest.HeadRefOid
	result.MergeStateRaw = q.Repository.PullRequest.MergeStateStatus

	attributeResult(&result, q, pr.Number)
	return result, nil
}

// FetchBranchRules returns the configured branch rules from the REST API.
// This is only called by `view --condition rules`.
func (b *GitHubBackend) FetchBranchRules(ctx context.Context, pr backend.PRCoords) ([]backend.BranchRule, error) {
	return fetchBranchRules(ctx, pr)
}

// ── Attribution ladder ───────────────────────────────────────────────────────

// attributeResult applies the plan's attribution ladder to populate result
// Blockers and Advisories based on the query response.
func attributeResult(result *core.CheckResult, q prMergeabilityQueryStruct, prNumber int) {
	mergeable := q.Repository.PullRequest.Mergeable
	mergeState := q.Repository.PullRequest.MergeStateStatus
	reviewDecision := q.Repository.PullRequest.ReviewDecision

	switch {
	case mergeable == mergeableUnknown || mergeState == mergeableUnknown:
		result.Blockers = append(result.Blockers, core.Condition{
			Kind:            core.ConditionMergeEligibilityPending,
			Severity:        core.SeverityBlocker,
			Title:           "Merge eligibility is still being computed",
			Detail:          "GitHub is calculating the merge state asynchronously.",
			SuggestedAction: "Wait 15-30 seconds and re-run `mergeable-please check`.",
		})

	case mergeable == "CONFLICTING" || mergeState == "DIRTY":
		result.Blockers = append(result.Blockers, core.Condition{
			Kind:            core.ConditionConflict,
			Severity:        core.SeverityBlocker,
			Title:           "Branch has merge conflicts",
			SuggestedAction: "Resolve conflicts: git fetch && git merge origin/<base>, fix conflicts, commit, push.",
		})

	case mergeState == "BEHIND":
		result.Blockers = append(result.Blockers, core.Condition{
			Kind:            core.ConditionBehindBase,
			Severity:        core.SeverityBlocker,
			Title:           "Branch is behind its base branch",
			SuggestedAction: "Rebase: git fetch && git rebase origin/<base> && git push --force-with-lease.",
		})

	case mergeState == "BLOCKED":
		contexts := q.Repository.PullRequest.StatusCheckRollup.Contexts
		attributeBlocked(result, contexts.Nodes, contexts.PageInfo.HasNextPage, reviewDecision, prNumber)

	default:
		// CLEAN / UNSTABLE / HAS_HOOKS — no blockers from merge state.
		// Still surface review-decision advisories for completeness.
		addReviewDecisionAdvisory(result, reviewDecision)
	}
}

// attributeBlocked decomposes a BLOCKED mergeStateStatus into specific conditions
// using the available fields from the same query response.
func attributeBlocked(
	result *core.CheckResult,
	nodes []checkContextNode,
	truncated bool,
	reviewDecision string,
	prNumber int,
) {
	attributed := false

	checksCmd := fmt.Sprintf("gh pr checks %d", prNumber)

	failing, pending := collectRequiredCheckNames(nodes)
	if len(failing) > 0 {
		result.Blockers = append(result.Blockers, core.Condition{
			Kind:            core.ConditionCheckFailing,
			Severity:        core.SeverityBlocker,
			Title:           "Required CI check(s) failing",
			Detail:          strings.Join(failing, ", "),
			SuggestedAction: "Fix the reported failures and push a new commit.",
			DrillInCmd:      checksCmd,
		})
		attributed = true
	}
	if len(pending) > 0 {
		result.Blockers = append(result.Blockers, core.Condition{
			Kind:            core.ConditionCheckPending,
			Severity:        core.SeverityBlocker,
			Title:           "Required CI check(s) still running",
			Detail:          strings.Join(pending, ", "),
			SuggestedAction: "Wait for checks to complete, then re-run `mergeable-please check`.",
			DrillInCmd:      checksCmd,
		})
		attributed = true
	}

	if truncated {
		result.Advisories = append(result.Advisories, core.Condition{
			Kind:       core.ConditionCheckTruncated,
			Severity:   core.SeverityAdvisory,
			Title:      "Status check list was truncated at 100 entries",
			Detail:     "GitHub returned more than 100 checks; only the first 100 were evaluated.",
			DrillInCmd: checksCmd,
		})
	}

	if addReviewDecisionAdvisory(result, reviewDecision) {
		attributed = true
	}

	if !attributed {
		result.Advisories = append(result.Advisories, core.Condition{
			Kind:     core.ConditionResidualRuleset,
			Severity: core.SeverityAdvisory,
			Title:    "Branch blocked by an unattributed ruleset condition",
			Detail: "A branch ruleset is blocking the merge but could not be attributed to a " +
				"specific known condition (e.g. signed commits, linear history, deployment gate).",
			DrillInCmd: "mergeable-please view --condition rules",
		})
	}
}

// addReviewDecisionAdvisory appends an advisory for REVIEW_REQUIRED or
// CHANGES_REQUESTED review decisions. Returns true if an advisory was added.
func addReviewDecisionAdvisory(result *core.CheckResult, reviewDecision string) bool {
	switch reviewDecision {
	case "REVIEW_REQUIRED":
		result.Advisories = append(result.Advisories, core.Condition{
			Kind:            core.ConditionApprovalRequired,
			Severity:        core.SeverityAdvisory,
			Title:           "Human approval is required",
			Detail:          "reviewDecision=REVIEW_REQUIRED",
			SuggestedAction: "Request a human review and wait for approval.",
		})
		return true

	case "CHANGES_REQUESTED":
		result.Advisories = append(result.Advisories, core.Condition{
			Kind:            core.ConditionChangesRequested,
			Severity:        core.SeverityAdvisory,
			Title:           "A reviewer has requested changes",
			Detail:          "reviewDecision=CHANGES_REQUESTED",
			SuggestedAction: "Address the requested changes. Configured reviewers are managed by the reviewer loop.",
		})
		return true
	}
	return false
}

// collectRequiredCheckNames partitions required checks into failing and pending
// lists. Only required checks (IsRequired=true) are considered.
func collectRequiredCheckNames(nodes []checkContextNode) ([]string, []string) {
	var failing, pending []string
	for _, n := range nodes {
		cr := n.CheckRun
		if cr.Name != "" && cr.IsRequired {
			switch {
			case cr.Status == "COMPLETED" && isFailingCheckConclusion(cr.Conclusion):
				failing = append(failing, cr.Name)
			case cr.Status != "COMPLETED":
				pending = append(pending, cr.Name)
			}
		}

		sc := n.StatusContext
		if sc.Context != "" && sc.IsRequired {
			switch sc.State {
			case "FAILURE", "ERROR":
				failing = append(failing, sc.Context)
			case "PENDING":
				pending = append(pending, sc.Context)
			}
		}
	}
	return failing, pending
}

// isFailingCheckConclusion reports whether a CheckRun conclusion represents a failure.
// SKIPPED and NEUTRAL are not considered failures.
func isFailingCheckConclusion(conclusion string) bool {
	switch conclusion {
	case "FAILURE", "TIMED_OUT", "ACTION_REQUIRED", "CANCELLED":
		return true
	}
	return false
}
