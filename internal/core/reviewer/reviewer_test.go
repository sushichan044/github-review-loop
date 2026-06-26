package reviewer_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/sushichan044/mergeable-please/internal/core/reviewer"
)

func baseTime() time.Time {
	return time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
}

func alice() reviewer.Identity {
	return reviewer.Identity{Type: reviewer.ReviewerTypeUser, Name: "alice"}
}

func aliceUpper() reviewer.Identity {
	return reviewer.Identity{Type: reviewer.ReviewerTypeUser, Name: "ALICE"}
}

func bob() reviewer.Identity {
	return reviewer.Identity{Type: reviewer.ReviewerTypeUser, Name: "bob"}
}

func copilot() reviewer.Identity {
	return reviewer.Identity{Type: reviewer.ReviewerTypeGitHubCopilot, Name: ""}
}

func alicePolicy(goal reviewer.Goal, maxRallies int) reviewer.Policy {
	return reviewer.Policy{
		Identity:   alice(),
		Goal:       goal,
		MaxRallies: maxRallies,
		Trigger:    "re-request",
	}
}

// ─── Rally counting ───────────────────────────────────────────────────────────

func TestEvaluate_RallyCount_ZeroTriggers(t *testing.T) {
	t.Parallel()

	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewer.TriggerAction{},
		Reviews:       []reviewer.Review{},
		Threads:       []reviewer.Thread{},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalApproved, 5), s)
	assert.Equal(t, 0, result.RallyCount)
}

func TestEvaluate_RallyCount_OneTrigger(t *testing.T) {
	t.Parallel()

	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers: []reviewer.TriggerAction{
			{Reviewer: alice(), At: baseTime()},
		},
		Reviews: []reviewer.Review{},
		Threads: []reviewer.Thread{},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalApproved, 5), s)
	assert.Equal(t, 1, result.RallyCount)
}

func TestEvaluate_RallyCount_MultipleTriggers(t *testing.T) {
	t.Parallel()

	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers: []reviewer.TriggerAction{
			{Reviewer: alice(), At: baseTime()},
			{Reviewer: alice(), At: baseTime().Add(time.Hour)},
			{Reviewer: alice(), At: baseTime().Add(2 * time.Hour)},
			{Reviewer: bob(), At: baseTime()}, // different reviewer — does not count
		},
		Reviews: []reviewer.Review{},
		Threads: []reviewer.Thread{},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalApproved, 10), s)
	assert.Equal(t, 3, result.RallyCount)
}

func TestEvaluate_RallyCount_CaseInsensitiveMatch(t *testing.T) {
	t.Parallel()

	// Policy uses lowercase "alice"; trigger uses uppercase "ALICE"
	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers: []reviewer.TriggerAction{
			{Reviewer: aliceUpper(), At: baseTime()},
		},
		Reviews: []reviewer.Review{},
		Threads: []reviewer.Thread{},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalApproved, 5), s)
	assert.Equal(t, 1, result.RallyCount)
}

// ─── Goal: Approved ───────────────────────────────────────────────────────────

func TestEvaluate_ApprovedGoal_LatestReviewApproved(t *testing.T) {
	t.Parallel()

	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewer.TriggerAction{{Reviewer: alice(), At: baseTime()}},
		Reviews: []reviewer.Review{
			{Reviewer: alice(), State: reviewer.ReviewStateApproved, CommitOID: "head1", At: baseTime()},
		},
		Threads: []reviewer.Thread{},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalApproved, 5), s)
	assert.True(t, result.GoalMet)
	assert.Equal(t, reviewer.PhaseGoalMet, result.Phase)
}

func TestEvaluate_ApprovedGoal_LatestIsChangesRequested(t *testing.T) {
	t.Parallel()

	// Earlier Approved, later ChangesRequested — latest wins
	s := reviewer.Snapshot{
		HeadCommitOID: "head2",
		Triggers:      []reviewer.TriggerAction{{Reviewer: alice(), At: baseTime()}},
		Reviews: []reviewer.Review{
			{Reviewer: alice(), State: reviewer.ReviewStateApproved, CommitOID: "head1", At: baseTime()},
			{
				Reviewer:  alice(),
				State:     reviewer.ReviewStateChangesRequested,
				CommitOID: "head2",
				At:        baseTime().Add(time.Hour),
			},
		},
		Threads: []reviewer.Thread{},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalApproved, 5), s)
	assert.False(t, result.GoalMet)
	assert.Equal(t, reviewer.PhaseActive, result.Phase)
}

func TestEvaluate_ApprovedGoal_PendingIsIgnored(t *testing.T) {
	t.Parallel()

	// Approved first, then Pending — Pending must be ignored; Approved is the latest non-pending
	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewer.TriggerAction{{Reviewer: alice(), At: baseTime()}},
		Reviews: []reviewer.Review{
			{Reviewer: alice(), State: reviewer.ReviewStateApproved, CommitOID: "head1", At: baseTime()},
			{
				Reviewer:  alice(),
				State:     reviewer.ReviewStatePending,
				CommitOID: "head1",
				At:        baseTime().Add(time.Hour),
			},
		},
		Threads: []reviewer.Thread{},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalApproved, 5), s)
	assert.True(t, result.GoalMet)
}

func TestEvaluate_ApprovedGoal_NoReviews(t *testing.T) {
	t.Parallel()

	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewer.TriggerAction{{Reviewer: alice(), At: baseTime()}},
		Reviews:       []reviewer.Review{},
		Threads:       []reviewer.Thread{},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalApproved, 5), s)
	assert.False(t, result.GoalMet)
}

func TestEvaluate_ApprovedGoal_OtherReviewerDoesNotCount(t *testing.T) {
	t.Parallel()

	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewer.TriggerAction{{Reviewer: alice(), At: baseTime()}},
		Reviews: []reviewer.Review{
			{Reviewer: bob(), State: reviewer.ReviewStateApproved, CommitOID: "head1", At: baseTime()},
		},
		Threads: []reviewer.Thread{},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalApproved, 5), s)
	assert.False(t, result.GoalMet)
}

// ─── Goal: AllConversationsResolved ──────────────────────────────────────────

func TestEvaluate_AllConversationsGoal_NoThreads(t *testing.T) {
	t.Parallel()

	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewer.TriggerAction{{Reviewer: alice(), At: baseTime()}},
		Reviews:       []reviewer.Review{},
		Threads:       []reviewer.Thread{},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalAllConversationsResolved, 5), s)
	assert.True(t, result.GoalMet)
}

func TestEvaluate_AllConversationsGoal_AllResolved(t *testing.T) {
	t.Parallel()

	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewer.TriggerAction{{Reviewer: alice(), At: baseTime()}},
		Reviews:       []reviewer.Review{},
		Threads: []reviewer.Thread{
			{Reviewer: alice(), Resolved: true},
			{Reviewer: alice(), Resolved: true},
		},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalAllConversationsResolved, 5), s)
	assert.True(t, result.GoalMet)
}

func TestEvaluate_AllConversationsGoal_OneUnresolved(t *testing.T) {
	t.Parallel()

	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewer.TriggerAction{{Reviewer: alice(), At: baseTime()}},
		Reviews:       []reviewer.Review{},
		Threads: []reviewer.Thread{
			{Reviewer: alice(), Resolved: true},
			{Reviewer: alice(), Resolved: false},
		},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalAllConversationsResolved, 5), s)
	assert.False(t, result.GoalMet)
}

func TestEvaluate_AllConversationsGoal_OtherReviewerThreadDoesNotCount(t *testing.T) {
	t.Parallel()

	// Bob has an unresolved thread; Alice should still be GoalMet
	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewer.TriggerAction{{Reviewer: alice(), At: baseTime()}},
		Reviews:       []reviewer.Review{},
		Threads: []reviewer.Thread{
			{Reviewer: alice(), Resolved: true},
			{Reviewer: bob(), Resolved: false}, // Bob's — irrelevant to Alice's policy
		},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalAllConversationsResolved, 5), s)
	assert.True(t, result.GoalMet)
}

// ─── Phase transitions ────────────────────────────────────────────────────────

func TestEvaluate_Phase_Active(t *testing.T) {
	t.Parallel()

	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewer.TriggerAction{{Reviewer: alice(), At: baseTime()}},
		Reviews:       []reviewer.Review{},
		Threads:       []reviewer.Thread{},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalApproved, 5), s)
	assert.Equal(t, reviewer.PhaseActive, result.Phase)
	assert.False(t, result.GoalMet)
}

func TestEvaluate_Phase_ExhaustedAtBoundary(t *testing.T) {
	t.Parallel()

	// RallyCount == MaxRallies → Exhausted
	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers: []reviewer.TriggerAction{
			{Reviewer: alice(), At: baseTime()},
			{Reviewer: alice(), At: baseTime().Add(time.Hour)},
			{Reviewer: alice(), At: baseTime().Add(2 * time.Hour)},
		},
		Reviews: []reviewer.Review{},
		Threads: []reviewer.Thread{},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalApproved, 3), s)
	assert.Equal(t, reviewer.PhaseExhausted, result.Phase)
	assert.Equal(t, 3, result.RallyCount)
}

func TestEvaluate_Phase_GoalMetTakesPriorityOverExhausted(t *testing.T) {
	t.Parallel()

	// Both goal met AND rally >= max — GoalMet wins
	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers: []reviewer.TriggerAction{
			{Reviewer: alice(), At: baseTime()},
			{Reviewer: alice(), At: baseTime().Add(time.Hour)},
		},
		Reviews: []reviewer.Review{
			{Reviewer: alice(), State: reviewer.ReviewStateApproved, CommitOID: "head1", At: baseTime()},
		},
		Threads: []reviewer.Thread{},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalApproved, 2), s)
	assert.Equal(t, reviewer.PhaseGoalMet, result.Phase)
	assert.True(t, result.GoalMet)
}

// ─── Re-request guard ─────────────────────────────────────────────────────────

func TestEvaluate_Guard_BlockedWhenTerminalGoalMet(t *testing.T) {
	t.Parallel()

	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewer.TriggerAction{{Reviewer: alice(), At: baseTime()}},
		Reviews: []reviewer.Review{
			{Reviewer: alice(), State: reviewer.ReviewStateApproved, CommitOID: "head1", At: baseTime()},
		},
		Threads: []reviewer.Thread{},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalApproved, 5), s)
	assert.Equal(t, reviewer.PhaseGoalMet, result.Phase)
	assert.False(t, result.CanRerequest)
}

func TestEvaluate_Guard_BlockedWhenTerminalExhausted(t *testing.T) {
	t.Parallel()

	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers: []reviewer.TriggerAction{
			{Reviewer: alice(), At: baseTime()},
		},
		Reviews: []reviewer.Review{},
		Threads: []reviewer.Thread{},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalApproved, 1), s)
	assert.Equal(t, reviewer.PhaseExhausted, result.Phase)
	assert.False(t, result.CanRerequest)
}

func TestEvaluate_Guard_BlockedWhenSameCommit(t *testing.T) {
	t.Parallel()

	// Active but last review OID == HeadCommitOID — no new commit
	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewer.TriggerAction{{Reviewer: alice(), At: baseTime()}},
		Reviews: []reviewer.Review{
			{Reviewer: alice(), State: reviewer.ReviewStateChangesRequested, CommitOID: "head1", At: baseTime()},
		},
		Threads: []reviewer.Thread{},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalApproved, 5), s)
	assert.Equal(t, reviewer.PhaseActive, result.Phase)
	assert.False(t, result.CanRerequest)
	assert.NotEmpty(t, result.BlockReason)
}

func TestEvaluate_Guard_AllowedWhenHeadAdvanced(t *testing.T) {
	t.Parallel()

	// Active and last review OID != HeadCommitOID — new commit pushed
	s := reviewer.Snapshot{
		HeadCommitOID: "head2",
		Triggers:      []reviewer.TriggerAction{{Reviewer: alice(), At: baseTime()}},
		Reviews: []reviewer.Review{
			{Reviewer: alice(), State: reviewer.ReviewStateChangesRequested, CommitOID: "head1", At: baseTime()},
		},
		Threads: []reviewer.Thread{},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalApproved, 5), s)
	assert.Equal(t, reviewer.PhaseActive, result.Phase)
	assert.True(t, result.CanRerequest)
	assert.Empty(t, result.BlockReason)
}

func TestEvaluate_Guard_AllowedWhenNoPriorReview(t *testing.T) {
	t.Parallel()

	// Active and no reviews at all — initial request is allowed
	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewer.TriggerAction{{Reviewer: alice(), At: baseTime()}},
		Reviews:       []reviewer.Review{},
		Threads:       []reviewer.Thread{},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalApproved, 5), s)
	assert.Equal(t, reviewer.PhaseActive, result.Phase)
	assert.True(t, result.CanRerequest)
	assert.Empty(t, result.BlockReason)
}

func TestEvaluate_Guard_PendingIgnoredForCommitCheck(t *testing.T) {
	t.Parallel()

	// Only review is Pending at head — should use latest non-pending for OID check;
	// no non-pending review exists, so guard allows (same as no prior review)
	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewer.TriggerAction{{Reviewer: alice(), At: baseTime()}},
		Reviews: []reviewer.Review{
			{Reviewer: alice(), State: reviewer.ReviewStatePending, CommitOID: "head1", At: baseTime()},
		},
		Threads: []reviewer.Thread{},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalApproved, 5), s)
	assert.True(t, result.CanRerequest)
}

// ─── github-copilot identity ──────────────────────────────────────────────────

func TestEvaluate_CopilotIdentity_EmptyNameMatches(t *testing.T) {
	t.Parallel()

	policy := reviewer.Policy{
		Identity:   copilot(),
		Goal:       reviewer.GoalApproved,
		MaxRallies: 5,
		Trigger:    "re-request",
	}
	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers: []reviewer.TriggerAction{
			{Reviewer: copilot(), At: baseTime()},
		},
		Reviews: []reviewer.Review{
			{Reviewer: copilot(), State: reviewer.ReviewStateApproved, CommitOID: "head1", At: baseTime()},
		},
		Threads: []reviewer.Thread{},
	}
	result := reviewer.Evaluate(policy, s)
	assert.Equal(t, 1, result.RallyCount)
	assert.True(t, result.GoalMet)
}

// ─── EvaluateLoop ────────────────────────────────────────────────────────────

func TestEvaluateLoop_DoneWhenAllTerminal(t *testing.T) {
	t.Parallel()

	policies := []reviewer.Policy{
		alicePolicy(reviewer.GoalApproved, 5),
		{Identity: bob(), Goal: reviewer.GoalApproved, MaxRallies: 5, Trigger: "re-request"},
	}
	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers: []reviewer.TriggerAction{
			{Reviewer: alice(), At: baseTime()},
			{Reviewer: bob(), At: baseTime()},
		},
		Reviews: []reviewer.Review{
			{Reviewer: alice(), State: reviewer.ReviewStateApproved, CommitOID: "head1", At: baseTime()},
			{Reviewer: bob(), State: reviewer.ReviewStateApproved, CommitOID: "head1", At: baseTime()},
		},
		Threads: []reviewer.Thread{},
	}
	loop := reviewer.EvaluateLoop(policies, s)
	assert.True(t, loop.Done)
	assert.Len(t, loop.Reviewers, 2)
}

func TestEvaluateLoop_NotDoneWhenOneActive(t *testing.T) {
	t.Parallel()

	policies := []reviewer.Policy{
		alicePolicy(reviewer.GoalApproved, 5),
		{Identity: bob(), Goal: reviewer.GoalApproved, MaxRallies: 5, Trigger: "re-request"},
	}
	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers: []reviewer.TriggerAction{
			{Reviewer: alice(), At: baseTime()},
			{Reviewer: bob(), At: baseTime()},
		},
		Reviews: []reviewer.Review{
			{Reviewer: alice(), State: reviewer.ReviewStateApproved, CommitOID: "head1", At: baseTime()},
			// Bob has not approved yet
		},
		Threads: []reviewer.Thread{},
	}
	loop := reviewer.EvaluateLoop(policies, s)
	assert.False(t, loop.Done)
	assert.Len(t, loop.Reviewers, 2)
}

func TestEvaluateLoop_DoneMixedTerminalPhases(t *testing.T) {
	t.Parallel()

	// Alice is GoalMet, Bob is Exhausted — both terminal → Done
	policies := []reviewer.Policy{
		alicePolicy(reviewer.GoalApproved, 5),
		{Identity: bob(), Goal: reviewer.GoalApproved, MaxRallies: 1, Trigger: "re-request"},
	}
	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers: []reviewer.TriggerAction{
			{Reviewer: alice(), At: baseTime()},
			{Reviewer: bob(), At: baseTime()}, // 1 trigger == MaxRallies → Exhausted
		},
		Reviews: []reviewer.Review{
			{Reviewer: alice(), State: reviewer.ReviewStateApproved, CommitOID: "head1", At: baseTime()},
		},
		Threads: []reviewer.Thread{},
	}
	loop := reviewer.EvaluateLoop(policies, s)
	assert.True(t, loop.Done)
}

func TestEvaluateLoop_EmptyPolicies(t *testing.T) {
	t.Parallel()

	s := reviewer.Snapshot{HeadCommitOID: "head1"}
	loop := reviewer.EvaluateLoop([]reviewer.Policy{}, s)
	assert.True(t, loop.Done) // vacuously true — no non-terminal reviewers
	assert.Empty(t, loop.Reviewers)
}
