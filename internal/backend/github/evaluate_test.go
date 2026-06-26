package github_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sushichan044/mergeable-please/internal/backend"
	"github.com/sushichan044/mergeable-please/internal/backend/github"
	"github.com/sushichan044/mergeable-please/internal/core"
)

// fakeQuerier implements GraphQLQuerier and returns a fixed prMergeQueryResult.
type fakeEvalQuerier struct {
	result github.ExportedFakePRMergeResult
	err    error
}

func (f *fakeEvalQuerier) Query(_ string, q any, _ map[string]any) error {
	if f.err != nil {
		return f.err
	}
	github.InjectPRMergeResult(q, f.result)
	return nil
}

func newEvalClient(result github.ExportedFakePRMergeResult) *github.Client {
	return github.NewClientWithQuerier(&fakeEvalQuerier{result: result})
}

func pr() backend.PRCoords {
	return backend.PRCoords{Owner: "org", Repo: "repo", Number: 1}
}

// ── Attribution ladder tests ─────────────────────────────────────────────────

func TestBundledEvaluate_UNKNOWN_ReturnsMergeEligibilityPending(t *testing.T) {
	t.Parallel()

	client := newEvalClient(github.ExportedFakePRMergeResult{
		Mergeable:        "UNKNOWN",
		MergeStateStatus: "UNKNOWN",
	})
	be := github.NewBackend(client, github.WithRetrySleeper(noSleep))

	result, err := be.BundledEvaluate(context.Background(), pr())
	require.NoError(t, err)
	assert.False(t, result.Satisfied)
	require.Len(t, result.Blockers, 1)
	assert.Equal(t, core.ConditionMergeEligibilityPending, result.Blockers[0].Kind)
}

func TestBundledEvaluate_CONFLICTING_ReturnsConflictBlocker(t *testing.T) {
	t.Parallel()

	client := newEvalClient(github.ExportedFakePRMergeResult{
		Mergeable:        "CONFLICTING",
		MergeStateStatus: "DIRTY",
	})
	be := github.NewBackend(client)

	result, err := be.BundledEvaluate(context.Background(), pr())
	require.NoError(t, err)
	require.Len(t, result.Blockers, 1)
	assert.Equal(t, core.ConditionConflict, result.Blockers[0].Kind)
	assert.Equal(t, core.SeverityBlocker, result.Blockers[0].Severity)
}

func TestBundledEvaluate_BEHIND_ReturnsBehindBlocker(t *testing.T) {
	t.Parallel()

	client := newEvalClient(github.ExportedFakePRMergeResult{
		Mergeable:        "MERGEABLE",
		MergeStateStatus: "BEHIND",
	})
	be := github.NewBackend(client)

	result, err := be.BundledEvaluate(context.Background(), pr())
	require.NoError(t, err)
	require.Len(t, result.Blockers, 1)
	assert.Equal(t, core.ConditionBehindBase, result.Blockers[0].Kind)
	assert.Equal(t, core.SeverityBlocker, result.Blockers[0].Severity)
}

func TestBundledEvaluate_FailingRequiredCheck_ReturnsCheckFailingBlocker(t *testing.T) {
	t.Parallel()

	client := newEvalClient(github.ExportedFakePRMergeResult{
		Mergeable:        "MERGEABLE",
		MergeStateStatus: "BLOCKED",
		Checks: []github.ExportedFakeCheck{
			{Name: "lint", Status: "COMPLETED", Conclusion: "FAILURE", IsRequired: true},
			{Name: "build", Status: "COMPLETED", Conclusion: "SUCCESS", IsRequired: true},
		},
	})
	be := github.NewBackend(client)

	result, err := be.BundledEvaluate(context.Background(), pr())
	require.NoError(t, err)
	require.Len(t, result.Blockers, 1)
	assert.Equal(t, core.ConditionCheckFailing, result.Blockers[0].Kind)
	assert.Contains(t, result.Blockers[0].Detail, "lint")
}

func TestBundledEvaluate_PendingRequiredCheck_ReturnsCheckPendingBlocker(t *testing.T) {
	t.Parallel()

	client := newEvalClient(github.ExportedFakePRMergeResult{
		Mergeable:        "MERGEABLE",
		MergeStateStatus: "BLOCKED",
		Checks: []github.ExportedFakeCheck{
			{Name: "slow-test", Status: "IN_PROGRESS", Conclusion: "", IsRequired: true},
		},
	})
	be := github.NewBackend(client)

	result, err := be.BundledEvaluate(context.Background(), pr())
	require.NoError(t, err)
	require.Len(t, result.Blockers, 1)
	assert.Equal(t, core.ConditionCheckPending, result.Blockers[0].Kind)
	assert.Contains(t, result.Blockers[0].Detail, "slow-test")
}

func TestBundledEvaluate_ReviewRequired_ReturnsApprovalAdvisory(t *testing.T) {
	t.Parallel()

	client := newEvalClient(github.ExportedFakePRMergeResult{
		Mergeable:        "MERGEABLE",
		MergeStateStatus: "BLOCKED",
		ReviewDecision:   "REVIEW_REQUIRED",
	})
	be := github.NewBackend(client)

	result, err := be.BundledEvaluate(context.Background(), pr())
	require.NoError(t, err)
	assert.Empty(t, result.Blockers, "REVIEW_REQUIRED is advisory only")
	require.Len(t, result.Advisories, 1)
	assert.Equal(t, core.ConditionApprovalRequired, result.Advisories[0].Kind)
	assert.Equal(t, core.SeverityAdvisory, result.Advisories[0].Severity)
}

func TestBundledEvaluate_ChangesRequested_ReturnsChangesRequestedAdvisory(t *testing.T) {
	t.Parallel()

	client := newEvalClient(github.ExportedFakePRMergeResult{
		Mergeable:        "MERGEABLE",
		MergeStateStatus: "BLOCKED",
		ReviewDecision:   "CHANGES_REQUESTED",
	})
	be := github.NewBackend(client)

	result, err := be.BundledEvaluate(context.Background(), pr())
	require.NoError(t, err)
	assert.Empty(t, result.Blockers, "CHANGES_REQUESTED is advisory only")
	require.Len(t, result.Advisories, 1)
	assert.Equal(t, core.ConditionChangesRequested, result.Advisories[0].Kind)
}

func TestBundledEvaluate_ResidualBlocked_ReturnsResidualAdvisory(t *testing.T) {
	t.Parallel()

	// BLOCKED with no attributable cause → residual-ruleset advisory.
	client := newEvalClient(github.ExportedFakePRMergeResult{
		Mergeable:        "MERGEABLE",
		MergeStateStatus: "BLOCKED",
		ReviewDecision:   "APPROVED",
	})
	be := github.NewBackend(client)

	result, err := be.BundledEvaluate(context.Background(), pr())
	require.NoError(t, err)
	assert.Empty(t, result.Blockers)
	require.Len(t, result.Advisories, 1)
	assert.Equal(t, core.ConditionResidualRuleset, result.Advisories[0].Kind)
}

func TestBundledEvaluate_CLEAN_IsSatisfied(t *testing.T) {
	t.Parallel()

	client := newEvalClient(github.ExportedFakePRMergeResult{
		Mergeable:        "MERGEABLE",
		MergeStateStatus: "CLEAN",
	})
	be := github.NewBackend(client)

	result, err := be.BundledEvaluate(context.Background(), pr())
	require.NoError(t, err)
	assert.Empty(t, result.Blockers)
	assert.Empty(t, result.Advisories)
}

func TestBundledEvaluate_UNSTABLE_IsSatisfied(t *testing.T) {
	t.Parallel()

	// UNSTABLE means non-required checks failed — not our concern.
	client := newEvalClient(github.ExportedFakePRMergeResult{
		Mergeable:        "MERGEABLE",
		MergeStateStatus: "UNSTABLE",
	})
	be := github.NewBackend(client)

	result, err := be.BundledEvaluate(context.Background(), pr())
	require.NoError(t, err)
	assert.Empty(t, result.Blockers)
}

func TestBundledEvaluate_HAS_HOOKS_IsSatisfied(t *testing.T) {
	t.Parallel()

	client := newEvalClient(github.ExportedFakePRMergeResult{
		Mergeable:        "MERGEABLE",
		MergeStateStatus: "HAS_HOOKS",
	})
	be := github.NewBackend(client)

	result, err := be.BundledEvaluate(context.Background(), pr())
	require.NoError(t, err)
	assert.Empty(t, result.Blockers)
}

func TestBundledEvaluate_HeadCommitOIDCarried(t *testing.T) {
	t.Parallel()

	client := newEvalClient(github.ExportedFakePRMergeResult{
		Mergeable:        "MERGEABLE",
		MergeStateStatus: "CLEAN",
		HeadRefOid:       "abc123",
	})
	be := github.NewBackend(client)

	result, err := be.BundledEvaluate(context.Background(), pr())
	require.NoError(t, err)
	assert.Equal(t, "abc123", result.HeadCommitOID)
}

func TestBundledEvaluate_UNKNOWN_RetriesUntilExhausted(t *testing.T) {
	t.Parallel()

	// Query always returns UNKNOWN — retries exhaust, last result returned.
	calls := 0
	sleepCalls := 0

	client := github.NewClientWithQuerier(&fakeEvalQuerier{
		result: github.ExportedFakePRMergeResult{
			Mergeable:        "UNKNOWN",
			MergeStateStatus: "UNKNOWN",
		},
	})
	_ = calls

	sleeper := func(_ time.Duration) { sleepCalls++ }
	be := github.NewBackend(client, github.WithRetrySleeper(sleeper), github.WithRetryCount(2))

	result, err := be.BundledEvaluate(context.Background(), pr())
	require.NoError(t, err)
	assert.Equal(t, 2, sleepCalls, "should sleep between retries")
	assert.Equal(t, core.ConditionMergeEligibilityPending, result.Blockers[0].Kind)
}

// noSleep is an injectable sleeper that does nothing, for tests that trigger retry paths.
func noSleep(_ time.Duration) {}
