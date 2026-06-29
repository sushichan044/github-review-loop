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

// ─── Goal: ReviewedClean ─────────────────────────────────────────────────────

func TestEvaluate_ReviewedClean_NoReview_NotMet(t *testing.T) {
	t.Parallel()

	// No non-pending review → the reviewer has not signed off → not met.
	s := reviewer.Snapshot{HeadCommitOID: "head1"}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalReviewedClean, 5), s)
	assert.False(t, result.GoalMet)
}

func TestEvaluate_ReviewedClean_CommentedZeroInlineOnHead_Met(t *testing.T) {
	t.Parallel()

	// A COMMENTED review on the current head with no inline findings is a clean
	// sign-off → met. Threads are irrelevant to this goal.
	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Reviews: []reviewer.Review{
			{
				Reviewer:           alice(),
				State:              reviewer.ReviewStateCommented,
				CommitOID:          "head1",
				At:                 baseTime(),
				InlineCommentCount: 0,
			},
		},
		Threads: []reviewer.Thread{{Reviewer: alice(), Resolved: false}}, // unresolved, but ignored
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalReviewedClean, 5), s)
	assert.True(t, result.GoalMet)
}

func TestEvaluate_ReviewedClean_CommentedWithInlineComments_NotMet(t *testing.T) {
	t.Parallel()

	// A review on head that left inline findings is not a clean sign-off.
	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Reviews: []reviewer.Review{
			{
				Reviewer:           alice(),
				State:              reviewer.ReviewStateCommented,
				CommitOID:          "head1",
				At:                 baseTime(),
				InlineCommentCount: 3,
			},
		},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalReviewedClean, 5), s)
	assert.False(t, result.GoalMet)
}

func TestEvaluate_ReviewedClean_ApprovedOnHead_Met(t *testing.T) {
	t.Parallel()

	// An approval on head meets the goal even if that review carried inline notes.
	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Reviews: []reviewer.Review{
			{
				Reviewer:           alice(),
				State:              reviewer.ReviewStateApproved,
				CommitOID:          "head1",
				At:                 baseTime(),
				InlineCommentCount: 2,
			},
		},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalReviewedClean, 5), s)
	assert.True(t, result.GoalMet)
}

func TestEvaluate_ReviewedClean_CleanReviewOnOldCommit_NotMet(t *testing.T) {
	t.Parallel()

	// A clean review of an older commit is stale once new commits are pushed.
	s := reviewer.Snapshot{
		HeadCommitOID: "head2",
		Reviews: []reviewer.Review{
			{
				Reviewer:           alice(),
				State:              reviewer.ReviewStateCommented,
				CommitOID:          "head1",
				At:                 baseTime(),
				InlineCommentCount: 0,
			},
		},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalReviewedClean, 5), s)
	assert.False(t, result.GoalMet, "clean review must be on the current head")
}

func TestEvaluate_ReviewedClean_ChangesRequestedOnHead_NotMetAndFlagged(t *testing.T) {
	t.Parallel()

	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Reviews: []reviewer.Review{
			{
				Reviewer:           alice(),
				State:              reviewer.ReviewStateChangesRequested,
				CommitOID:          "head1",
				At:                 baseTime(),
				InlineCommentCount: 0,
			},
		},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalReviewedClean, 5), s)
	assert.False(t, result.GoalMet, "changes-requested is never a clean sign-off")
	assert.True(t, result.ChangesRequested)
}

func TestEvaluate_ReviewedClean_LatestReviewWins(t *testing.T) {
	t.Parallel()

	// An older changes-requested review superseded by a newer clean review on head
	// → met; only the latest non-pending review counts.
	s := reviewer.Snapshot{
		HeadCommitOID: "head2",
		Reviews: []reviewer.Review{
			{Reviewer: alice(), State: reviewer.ReviewStateChangesRequested, CommitOID: "head1", At: baseTime()},
			{
				Reviewer:           alice(),
				State:              reviewer.ReviewStateCommented,
				CommitOID:          "head2",
				At:                 baseTime().Add(time.Hour),
				InlineCommentCount: 0,
			},
		},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalReviewedClean, 5), s)
	assert.True(t, result.GoalMet)
	assert.False(t, result.ChangesRequested, "newer clean review supersedes the older changes-requested")
}

func TestEvaluate_ReviewedClean_OtherReviewerDoesNotCount(t *testing.T) {
	t.Parallel()

	// Only the policy reviewer's own reviews count.
	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Reviews: []reviewer.Review{
			{Reviewer: bob(), State: reviewer.ReviewStateApproved, CommitOID: "head1", At: baseTime()},
		},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalReviewedClean, 5), s)
	assert.False(t, result.GoalMet)
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

func TestEvaluate_Guard_AllowedWhenNeverRequested(t *testing.T) {
	t.Parallel()

	// Active, no reviews, and no prior trigger → truly initial request, allowed.
	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewer.TriggerAction{},
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

	// Trigger exists + only a pending review → outstanding request (no non-pending
	// response) → blocked, not allowed.
	s := reviewer.Snapshot{
		HeadCommitOID: "head1",
		Triggers:      []reviewer.TriggerAction{{Reviewer: alice(), At: baseTime()}},
		Reviews: []reviewer.Review{
			{Reviewer: alice(), State: reviewer.ReviewStatePending, CommitOID: "head1", At: baseTime()},
		},
		Threads: []reviewer.Thread{},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalApproved, 5), s)
	assert.False(t, result.CanRerequest)
	assert.NotEmpty(t, result.BlockReason)
}

// ─── Stale approval ──────────────────────────────────────────────────────────

func TestEvaluate_ApprovedGoal_StaleApprovalIsNotTerminal(t *testing.T) {
	t.Parallel()

	// Approved at an older commit while head has advanced → stale → goal NOT met,
	// reviewer stays active and can be re-requested once new commits land.
	s := reviewer.Snapshot{
		HeadCommitOID: "head2",
		Reviews: []reviewer.Review{
			{Reviewer: alice(), State: reviewer.ReviewStateApproved, CommitOID: "head1", At: baseTime()},
		},
		Threads: []reviewer.Thread{},
	}
	result := reviewer.Evaluate(alicePolicy(reviewer.GoalApproved, 5), s)
	assert.False(t, result.GoalMet, "approval on old commit must not satisfy goal")
	assert.Equal(t, reviewer.PhaseActive, result.Phase, "reviewer must return to active after new commits")
}

// ─── Outstanding-request guard (4-case matrix) ───────────────────────────────

func TestEvaluate_Guard_OutstandingRequestMatrix(t *testing.T) {
	t.Parallel()

	type tc struct {
		name             string
		triggers         []reviewer.TriggerAction
		reviews          []reviewer.Review
		wantCanRerequest bool
		wantBlockReason  string // non-empty substring expected in BlockReason when blocked
	}

	t0 := baseTime()
	t1 := t0.Add(time.Hour)

	tests := []tc{
		{
			name:             "never-engaged never-requested: initial request allowed",
			triggers:         []reviewer.TriggerAction{},
			reviews:          []reviewer.Review{},
			wantCanRerequest: true,
		},
		{
			name:             "never-engaged already-requested: awaiting response",
			triggers:         []reviewer.TriggerAction{{Reviewer: alice(), At: t0}},
			reviews:          []reviewer.Review{},
			wantCanRerequest: false,
			wantBlockReason:  "already requested",
		},
		{
			name: "engaged head-not-advanced: same commit blocks re-request",
			triggers: []reviewer.TriggerAction{
				{Reviewer: alice(), At: t0},
			},
			reviews: []reviewer.Review{
				{Reviewer: alice(), State: reviewer.ReviewStateChangesRequested, CommitOID: "head1", At: t1},
			},
			wantCanRerequest: false,
			wantBlockReason:  "no new commit",
		},
		{
			name: "engaged head-advanced no-outstanding: re-request allowed",
			triggers: []reviewer.TriggerAction{
				{Reviewer: alice(), At: t0},
			},
			reviews: []reviewer.Review{
				{Reviewer: alice(), State: reviewer.ReviewStateChangesRequested, CommitOID: "head1", At: t1},
			},
			// Override HeadCommitOID to head2 in snapshot below.
			wantCanRerequest: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			headOID := "head1"
			if tt.wantCanRerequest && len(tt.reviews) > 0 {
				// advance head so commit guard does not fire
				headOID = "head2"
			}

			s := reviewer.Snapshot{
				HeadCommitOID: headOID,
				Triggers:      tt.triggers,
				Reviews:       tt.reviews,
				Threads:       []reviewer.Thread{},
			}
			result := reviewer.Evaluate(alicePolicy(reviewer.GoalApproved, 5), s)

			assert.Equal(t, tt.wantCanRerequest, result.CanRerequest, "CanRerequest")
			if tt.wantBlockReason != "" {
				assert.Contains(t, result.BlockReason, tt.wantBlockReason, "BlockReason")
			}
		})
	}
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
