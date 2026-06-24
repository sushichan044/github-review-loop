package reviewloop_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/sushichan044/github-review-loop/internal/reviewloop"
)

var (
	baseTime   = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	alice      = reviewloop.ReviewerIdentity{Type: reviewloop.ReviewerTypeUser, Name: "alice"}
	aliceUpper = reviewloop.ReviewerIdentity{Type: reviewloop.ReviewerTypeUser, Name: "ALICE"}
	bob        = reviewloop.ReviewerIdentity{Type: reviewloop.ReviewerTypeUser, Name: "bob"}
	copilot    = reviewloop.ReviewerIdentity{Type: reviewloop.ReviewerTypeGitHubCopilot, Name: ""}
)

func alicePolicy(goal reviewloop.Goal, maxRallies int) reviewloop.Policy {
	return reviewloop.Policy{
		Identity:   alice,
		Goal:       goal,
		MaxRallies: maxRallies,
		Trigger:    "re-request",
	}
}

// ─── Rally counting ───────────────────────────────────────────────────────────

func TestEvaluate_RallyCount_ZeroTriggers(t *testing.T) {
	t.Parallel()

	s := reviewloop.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewloop.TriggerAction{},
		Reviews:       []reviewloop.Review{},
		Threads:       []reviewloop.Thread{},
	}
	result := reviewloop.Evaluate(alicePolicy(reviewloop.GoalApproved, 5), s)
	assert.Equal(t, 0, result.RallyCount)
}

func TestEvaluate_RallyCount_OneTrigger(t *testing.T) {
	t.Parallel()

	s := reviewloop.Snapshot{
		HeadCommitOID: "head1",
		Triggers: []reviewloop.TriggerAction{
			{Reviewer: alice, At: baseTime},
		},
		Reviews: []reviewloop.Review{},
		Threads: []reviewloop.Thread{},
	}
	result := reviewloop.Evaluate(alicePolicy(reviewloop.GoalApproved, 5), s)
	assert.Equal(t, 1, result.RallyCount)
}

func TestEvaluate_RallyCount_MultipleTriggers(t *testing.T) {
	t.Parallel()

	s := reviewloop.Snapshot{
		HeadCommitOID: "head1",
		Triggers: []reviewloop.TriggerAction{
			{Reviewer: alice, At: baseTime},
			{Reviewer: alice, At: baseTime.Add(time.Hour)},
			{Reviewer: alice, At: baseTime.Add(2 * time.Hour)},
			{Reviewer: bob, At: baseTime}, // different reviewer — does not count
		},
		Reviews: []reviewloop.Review{},
		Threads: []reviewloop.Thread{},
	}
	result := reviewloop.Evaluate(alicePolicy(reviewloop.GoalApproved, 10), s)
	assert.Equal(t, 3, result.RallyCount)
}

func TestEvaluate_RallyCount_CaseInsensitiveMatch(t *testing.T) {
	t.Parallel()

	// Policy uses lowercase "alice"; trigger uses uppercase "ALICE"
	s := reviewloop.Snapshot{
		HeadCommitOID: "head1",
		Triggers: []reviewloop.TriggerAction{
			{Reviewer: aliceUpper, At: baseTime},
		},
		Reviews: []reviewloop.Review{},
		Threads: []reviewloop.Thread{},
	}
	result := reviewloop.Evaluate(alicePolicy(reviewloop.GoalApproved, 5), s)
	assert.Equal(t, 1, result.RallyCount)
}

// ─── Goal: Approved ───────────────────────────────────────────────────────────

func TestEvaluate_ApprovedGoal_LatestReviewApproved(t *testing.T) {
	t.Parallel()

	s := reviewloop.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewloop.TriggerAction{{Reviewer: alice, At: baseTime}},
		Reviews: []reviewloop.Review{
			{Reviewer: alice, State: reviewloop.ReviewStateApproved, CommitOID: "head1", At: baseTime},
		},
		Threads: []reviewloop.Thread{},
	}
	result := reviewloop.Evaluate(alicePolicy(reviewloop.GoalApproved, 5), s)
	assert.True(t, result.GoalMet)
	assert.Equal(t, reviewloop.PhaseGoalMet, result.Phase)
}

func TestEvaluate_ApprovedGoal_LatestIsChangesRequested(t *testing.T) {
	t.Parallel()

	// Earlier Approved, later ChangesRequested — latest wins
	s := reviewloop.Snapshot{
		HeadCommitOID: "head2",
		Triggers:      []reviewloop.TriggerAction{{Reviewer: alice, At: baseTime}},
		Reviews: []reviewloop.Review{
			{Reviewer: alice, State: reviewloop.ReviewStateApproved, CommitOID: "head1", At: baseTime},
			{
				Reviewer:  alice,
				State:     reviewloop.ReviewStateChangesRequested,
				CommitOID: "head2",
				At:        baseTime.Add(time.Hour),
			},
		},
		Threads: []reviewloop.Thread{},
	}
	result := reviewloop.Evaluate(alicePolicy(reviewloop.GoalApproved, 5), s)
	assert.False(t, result.GoalMet)
	assert.Equal(t, reviewloop.PhaseActive, result.Phase)
}

func TestEvaluate_ApprovedGoal_PendingIsIgnored(t *testing.T) {
	t.Parallel()

	// Approved first, then Pending — Pending must be ignored; Approved is the latest non-pending
	s := reviewloop.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewloop.TriggerAction{{Reviewer: alice, At: baseTime}},
		Reviews: []reviewloop.Review{
			{Reviewer: alice, State: reviewloop.ReviewStateApproved, CommitOID: "head1", At: baseTime},
			{Reviewer: alice, State: reviewloop.ReviewStatePending, CommitOID: "head1", At: baseTime.Add(time.Hour)},
		},
		Threads: []reviewloop.Thread{},
	}
	result := reviewloop.Evaluate(alicePolicy(reviewloop.GoalApproved, 5), s)
	assert.True(t, result.GoalMet)
}

func TestEvaluate_ApprovedGoal_NoReviews(t *testing.T) {
	t.Parallel()

	s := reviewloop.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewloop.TriggerAction{{Reviewer: alice, At: baseTime}},
		Reviews:       []reviewloop.Review{},
		Threads:       []reviewloop.Thread{},
	}
	result := reviewloop.Evaluate(alicePolicy(reviewloop.GoalApproved, 5), s)
	assert.False(t, result.GoalMet)
}

func TestEvaluate_ApprovedGoal_OtherReviewerDoesNotCount(t *testing.T) {
	t.Parallel()

	s := reviewloop.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewloop.TriggerAction{{Reviewer: alice, At: baseTime}},
		Reviews: []reviewloop.Review{
			{Reviewer: bob, State: reviewloop.ReviewStateApproved, CommitOID: "head1", At: baseTime},
		},
		Threads: []reviewloop.Thread{},
	}
	result := reviewloop.Evaluate(alicePolicy(reviewloop.GoalApproved, 5), s)
	assert.False(t, result.GoalMet)
}

// ─── Goal: AllConversationsResolved ──────────────────────────────────────────

func TestEvaluate_AllConversationsGoal_NoThreads(t *testing.T) {
	t.Parallel()

	s := reviewloop.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewloop.TriggerAction{{Reviewer: alice, At: baseTime}},
		Reviews:       []reviewloop.Review{},
		Threads:       []reviewloop.Thread{},
	}
	result := reviewloop.Evaluate(alicePolicy(reviewloop.GoalAllConversationsResolved, 5), s)
	assert.True(t, result.GoalMet)
}

func TestEvaluate_AllConversationsGoal_AllResolved(t *testing.T) {
	t.Parallel()

	s := reviewloop.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewloop.TriggerAction{{Reviewer: alice, At: baseTime}},
		Reviews:       []reviewloop.Review{},
		Threads: []reviewloop.Thread{
			{Reviewer: alice, Resolved: true},
			{Reviewer: alice, Resolved: true},
		},
	}
	result := reviewloop.Evaluate(alicePolicy(reviewloop.GoalAllConversationsResolved, 5), s)
	assert.True(t, result.GoalMet)
}

func TestEvaluate_AllConversationsGoal_OneUnresolved(t *testing.T) {
	t.Parallel()

	s := reviewloop.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewloop.TriggerAction{{Reviewer: alice, At: baseTime}},
		Reviews:       []reviewloop.Review{},
		Threads: []reviewloop.Thread{
			{Reviewer: alice, Resolved: true},
			{Reviewer: alice, Resolved: false},
		},
	}
	result := reviewloop.Evaluate(alicePolicy(reviewloop.GoalAllConversationsResolved, 5), s)
	assert.False(t, result.GoalMet)
}

func TestEvaluate_AllConversationsGoal_OtherReviewerThreadDoesNotCount(t *testing.T) {
	t.Parallel()

	// Bob has an unresolved thread; Alice should still be GoalMet
	s := reviewloop.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewloop.TriggerAction{{Reviewer: alice, At: baseTime}},
		Reviews:       []reviewloop.Review{},
		Threads: []reviewloop.Thread{
			{Reviewer: alice, Resolved: true},
			{Reviewer: bob, Resolved: false}, // Bob's — irrelevant to Alice's policy
		},
	}
	result := reviewloop.Evaluate(alicePolicy(reviewloop.GoalAllConversationsResolved, 5), s)
	assert.True(t, result.GoalMet)
}

// ─── Phase transitions ────────────────────────────────────────────────────────

func TestEvaluate_Phase_Active(t *testing.T) {
	t.Parallel()

	s := reviewloop.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewloop.TriggerAction{{Reviewer: alice, At: baseTime}},
		Reviews:       []reviewloop.Review{},
		Threads:       []reviewloop.Thread{},
	}
	result := reviewloop.Evaluate(alicePolicy(reviewloop.GoalApproved, 5), s)
	assert.Equal(t, reviewloop.PhaseActive, result.Phase)
	assert.False(t, result.GoalMet)
}

func TestEvaluate_Phase_ExhaustedAtBoundary(t *testing.T) {
	t.Parallel()

	// RallyCount == MaxRallies → Exhausted
	s := reviewloop.Snapshot{
		HeadCommitOID: "head1",
		Triggers: []reviewloop.TriggerAction{
			{Reviewer: alice, At: baseTime},
			{Reviewer: alice, At: baseTime.Add(time.Hour)},
			{Reviewer: alice, At: baseTime.Add(2 * time.Hour)},
		},
		Reviews: []reviewloop.Review{},
		Threads: []reviewloop.Thread{},
	}
	result := reviewloop.Evaluate(alicePolicy(reviewloop.GoalApproved, 3), s)
	assert.Equal(t, reviewloop.PhaseExhausted, result.Phase)
	assert.Equal(t, 3, result.RallyCount)
}

func TestEvaluate_Phase_GoalMetTakesPriorityOverExhausted(t *testing.T) {
	t.Parallel()

	// Both goal met AND rally >= max — GoalMet wins
	s := reviewloop.Snapshot{
		HeadCommitOID: "head1",
		Triggers: []reviewloop.TriggerAction{
			{Reviewer: alice, At: baseTime},
			{Reviewer: alice, At: baseTime.Add(time.Hour)},
		},
		Reviews: []reviewloop.Review{
			{Reviewer: alice, State: reviewloop.ReviewStateApproved, CommitOID: "head1", At: baseTime},
		},
		Threads: []reviewloop.Thread{},
	}
	result := reviewloop.Evaluate(alicePolicy(reviewloop.GoalApproved, 2), s)
	assert.Equal(t, reviewloop.PhaseGoalMet, result.Phase)
	assert.True(t, result.GoalMet)
}

// ─── Re-request guard ─────────────────────────────────────────────────────────

func TestEvaluate_Guard_BlockedWhenTerminalGoalMet(t *testing.T) {
	t.Parallel()

	s := reviewloop.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewloop.TriggerAction{{Reviewer: alice, At: baseTime}},
		Reviews: []reviewloop.Review{
			{Reviewer: alice, State: reviewloop.ReviewStateApproved, CommitOID: "head1", At: baseTime},
		},
		Threads: []reviewloop.Thread{},
	}
	result := reviewloop.Evaluate(alicePolicy(reviewloop.GoalApproved, 5), s)
	assert.Equal(t, reviewloop.PhaseGoalMet, result.Phase)
	assert.False(t, result.CanRerequest)
}

func TestEvaluate_Guard_BlockedWhenTerminalExhausted(t *testing.T) {
	t.Parallel()

	s := reviewloop.Snapshot{
		HeadCommitOID: "head1",
		Triggers: []reviewloop.TriggerAction{
			{Reviewer: alice, At: baseTime},
		},
		Reviews: []reviewloop.Review{},
		Threads: []reviewloop.Thread{},
	}
	result := reviewloop.Evaluate(alicePolicy(reviewloop.GoalApproved, 1), s)
	assert.Equal(t, reviewloop.PhaseExhausted, result.Phase)
	assert.False(t, result.CanRerequest)
}

func TestEvaluate_Guard_BlockedWhenSameCommit(t *testing.T) {
	t.Parallel()

	// Active but last review OID == HeadCommitOID — no new commit
	s := reviewloop.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewloop.TriggerAction{{Reviewer: alice, At: baseTime}},
		Reviews: []reviewloop.Review{
			{Reviewer: alice, State: reviewloop.ReviewStateChangesRequested, CommitOID: "head1", At: baseTime},
		},
		Threads: []reviewloop.Thread{},
	}
	result := reviewloop.Evaluate(alicePolicy(reviewloop.GoalApproved, 5), s)
	assert.Equal(t, reviewloop.PhaseActive, result.Phase)
	assert.False(t, result.CanRerequest)
	assert.NotEmpty(t, result.BlockReason)
}

func TestEvaluate_Guard_AllowedWhenHeadAdvanced(t *testing.T) {
	t.Parallel()

	// Active and last review OID != HeadCommitOID — new commit pushed
	s := reviewloop.Snapshot{
		HeadCommitOID: "head2",
		Triggers:      []reviewloop.TriggerAction{{Reviewer: alice, At: baseTime}},
		Reviews: []reviewloop.Review{
			{Reviewer: alice, State: reviewloop.ReviewStateChangesRequested, CommitOID: "head1", At: baseTime},
		},
		Threads: []reviewloop.Thread{},
	}
	result := reviewloop.Evaluate(alicePolicy(reviewloop.GoalApproved, 5), s)
	assert.Equal(t, reviewloop.PhaseActive, result.Phase)
	assert.True(t, result.CanRerequest)
	assert.Empty(t, result.BlockReason)
}

func TestEvaluate_Guard_AllowedWhenNoPriorReview(t *testing.T) {
	t.Parallel()

	// Active and no reviews at all — initial request is allowed
	s := reviewloop.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewloop.TriggerAction{{Reviewer: alice, At: baseTime}},
		Reviews:       []reviewloop.Review{},
		Threads:       []reviewloop.Thread{},
	}
	result := reviewloop.Evaluate(alicePolicy(reviewloop.GoalApproved, 5), s)
	assert.Equal(t, reviewloop.PhaseActive, result.Phase)
	assert.True(t, result.CanRerequest)
	assert.Empty(t, result.BlockReason)
}

func TestEvaluate_Guard_PendingIgnoredForCommitCheck(t *testing.T) {
	t.Parallel()

	// Only review is Pending at head — should use latest non-pending for OID check;
	// no non-pending review exists, so guard allows (same as no prior review)
	s := reviewloop.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewloop.TriggerAction{{Reviewer: alice, At: baseTime}},
		Reviews: []reviewloop.Review{
			{Reviewer: alice, State: reviewloop.ReviewStatePending, CommitOID: "head1", At: baseTime},
		},
		Threads: []reviewloop.Thread{},
	}
	result := reviewloop.Evaluate(alicePolicy(reviewloop.GoalApproved, 5), s)
	assert.True(t, result.CanRerequest)
}

// ─── github-copilot identity ──────────────────────────────────────────────────

func TestEvaluate_CopilotIdentity_EmptyNameMatches(t *testing.T) {
	t.Parallel()

	policy := reviewloop.Policy{
		Identity:   copilot,
		Goal:       reviewloop.GoalApproved,
		MaxRallies: 5,
		Trigger:    "re-request",
	}
	s := reviewloop.Snapshot{
		HeadCommitOID: "head1",
		Triggers: []reviewloop.TriggerAction{
			{Reviewer: copilot, At: baseTime},
		},
		Reviews: []reviewloop.Review{
			{Reviewer: copilot, State: reviewloop.ReviewStateApproved, CommitOID: "head1", At: baseTime},
		},
		Threads: []reviewloop.Thread{},
	}
	result := reviewloop.Evaluate(policy, s)
	assert.Equal(t, 1, result.RallyCount)
	assert.True(t, result.GoalMet)
}

// ─── EvaluateLoop ────────────────────────────────────────────────────────────

func TestEvaluateLoop_DoneWhenAllTerminal(t *testing.T) {
	t.Parallel()

	policies := []reviewloop.Policy{
		alicePolicy(reviewloop.GoalApproved, 5),
		{Identity: bob, Goal: reviewloop.GoalApproved, MaxRallies: 5, Trigger: "re-request"},
	}
	s := reviewloop.Snapshot{
		HeadCommitOID: "head1",
		Triggers: []reviewloop.TriggerAction{
			{Reviewer: alice, At: baseTime},
			{Reviewer: bob, At: baseTime},
		},
		Reviews: []reviewloop.Review{
			{Reviewer: alice, State: reviewloop.ReviewStateApproved, CommitOID: "head1", At: baseTime},
			{Reviewer: bob, State: reviewloop.ReviewStateApproved, CommitOID: "head1", At: baseTime},
		},
		Threads: []reviewloop.Thread{},
	}
	loop := reviewloop.EvaluateLoop(policies, s)
	assert.True(t, loop.Done)
	assert.Len(t, loop.Reviewers, 2)
}

func TestEvaluateLoop_NotDoneWhenOneActive(t *testing.T) {
	t.Parallel()

	policies := []reviewloop.Policy{
		alicePolicy(reviewloop.GoalApproved, 5),
		{Identity: bob, Goal: reviewloop.GoalApproved, MaxRallies: 5, Trigger: "re-request"},
	}
	s := reviewloop.Snapshot{
		HeadCommitOID: "head1",
		Triggers: []reviewloop.TriggerAction{
			{Reviewer: alice, At: baseTime},
			{Reviewer: bob, At: baseTime},
		},
		Reviews: []reviewloop.Review{
			{Reviewer: alice, State: reviewloop.ReviewStateApproved, CommitOID: "head1", At: baseTime},
			// Bob has not approved yet
		},
		Threads: []reviewloop.Thread{},
	}
	loop := reviewloop.EvaluateLoop(policies, s)
	assert.False(t, loop.Done)
	assert.Len(t, loop.Reviewers, 2)
}

func TestEvaluateLoop_DoneMixedTerminalPhases(t *testing.T) {
	t.Parallel()

	// Alice is GoalMet, Bob is Exhausted — both terminal → Done
	policies := []reviewloop.Policy{
		alicePolicy(reviewloop.GoalApproved, 5),
		{Identity: bob, Goal: reviewloop.GoalApproved, MaxRallies: 1, Trigger: "re-request"},
	}
	s := reviewloop.Snapshot{
		HeadCommitOID: "head1",
		Triggers: []reviewloop.TriggerAction{
			{Reviewer: alice, At: baseTime},
			{Reviewer: bob, At: baseTime}, // 1 trigger == MaxRallies → Exhausted
		},
		Reviews: []reviewloop.Review{
			{Reviewer: alice, State: reviewloop.ReviewStateApproved, CommitOID: "head1", At: baseTime},
		},
		Threads: []reviewloop.Thread{},
	}
	loop := reviewloop.EvaluateLoop(policies, s)
	assert.True(t, loop.Done)
}

func TestEvaluateLoop_EmptyPolicies(t *testing.T) {
	t.Parallel()

	s := reviewloop.Snapshot{HeadCommitOID: "head1"}
	loop := reviewloop.EvaluateLoop([]reviewloop.Policy{}, s)
	assert.True(t, loop.Done) // vacuously true — no non-terminal reviewers
	assert.Empty(t, loop.Reviewers)
}
