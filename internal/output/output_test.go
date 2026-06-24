package output_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sushichan044/github-review-loop/internal/output"
	"github.com/sushichan044/github-review-loop/internal/reviewloop"
)

// ── FormatFor ────────────────────────────────────────────────────────────────

func TestFormatFor(t *testing.T) {
	t.Parallel()

	assert.Equal(t, output.FormatAgent, output.FormatFor(true), "isAgent=true should yield FormatAgent")
	assert.Equal(t, output.FormatHuman, output.FormatFor(false), "isAgent=false should yield FormatHuman")
}

// ── DefaultFormat ─────────────────────────────────────────────────────────────

// TestDefaultFormat exercises DefaultFormat() to keep it reachable for deadcode
// analysis. The concrete agent/human mapping is the spec of TestFormatFor; because
// DefaultFormat() depends on the ambient environment (agentdetection.IsAgent()),
// this test only asserts the result is a valid Format — keeping it deterministic
// across both agent and non-agent (CI) environments.
func TestDefaultFormat(t *testing.T) {
	t.Parallel()

	f := output.DefaultFormat()
	_, err := output.ParseFormat(string(f))
	require.NoError(t, err, "DefaultFormat() must return a valid Format")
}

// ── ParseFormat ──────────────────────────────────────────────────────────────

func TestParseFormat(t *testing.T) {
	t.Parallel()

	t.Run("valid_agent", func(t *testing.T) {
		t.Parallel()
		f, err := output.ParseFormat("agent")
		require.NoError(t, err)
		assert.Equal(t, output.FormatAgent, f)
	})

	t.Run("valid_human", func(t *testing.T) {
		t.Parallel()
		f, err := output.ParseFormat("human")
		require.NoError(t, err)
		assert.Equal(t, output.FormatHuman, f)
	})

	t.Run("invalid", func(t *testing.T) {
		t.Parallel()
		_, err := output.ParseFormat("unknown")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown")
	})
}

// ── Render helpers ───────────────────────────────────────────────────────────

func renderString(t *testing.T, v output.LoopView, f output.Format) string {
	t.Helper()
	var sb strings.Builder
	err := output.Render(&sb, v, f)
	require.NoError(t, err)
	return sb.String()
}

func aliceIdentity() reviewloop.ReviewerIdentity {
	return reviewloop.ReviewerIdentity{
		Type: reviewloop.ReviewerTypeUser,
		Name: "alice",
	}
}

// ── GoalMet reviewer ─────────────────────────────────────────────────────────

func TestRender_GoalMet(t *testing.T) {
	t.Parallel()

	view := output.LoopView{
		Reviewers: []output.ReviewerView{
			{
				Identity:   aliceIdentity(),
				Goal:       reviewloop.GoalApproved,
				Phase:      reviewloop.PhaseGoalMet,
				RallyCount: 1,
				MaxRallies: 3,
				GoalMet:    true,
			},
		},
	}

	for _, f := range []output.Format{output.FormatHuman, output.FormatAgent} {
		t.Run(string(f), func(t *testing.T) {
			t.Parallel()
			out := renderString(t, view, f)

			assert.Contains(t, out, "goal-met", "phase should appear")
			assert.Contains(t, out, "alice", "reviewer name should appear")
			assert.Contains(t, out, "1/3", "rally count should appear")
			// next-action always present
			assert.Contains(t, out, "Goal met", "next-action: goal met message")
		})
	}
}

// ── Exhausted reviewer ───────────────────────────────────────────────────────

func TestRender_Exhausted(t *testing.T) {
	t.Parallel()

	view := output.LoopView{
		Reviewers: []output.ReviewerView{
			{
				Identity:   aliceIdentity(),
				Goal:       reviewloop.GoalApproved,
				Phase:      reviewloop.PhaseExhausted,
				RallyCount: 3,
				MaxRallies: 3,
				GoalMet:    false,
			},
		},
	}

	for _, f := range []output.Format{output.FormatHuman, output.FormatAgent} {
		t.Run(string(f), func(t *testing.T) {
			t.Parallel()
			out := renderString(t, view, f)

			assert.Contains(t, out, "exhausted", "phase should appear")
			assert.Contains(t, out, "3/3", "rally count should appear")
			// next-action always present with WARNING
			assert.Contains(t, out, "WARNING", "next-action: exhausted warning")
			assert.Contains(t, out, "max-rallies", "next-action: suggest raising max-rallies")
		})
	}
}

// ── Active + CanRerequest reviewer ───────────────────────────────────────────

func TestRender_Active_CanRerequest(t *testing.T) {
	t.Parallel()

	view := output.LoopView{
		Reviewers: []output.ReviewerView{
			{
				Identity:     aliceIdentity(),
				Goal:         reviewloop.GoalApproved,
				Phase:        reviewloop.PhaseActive,
				RallyCount:   1,
				MaxRallies:   3,
				GoalMet:      false,
				CanRerequest: true,
			},
		},
	}

	t.Run("human", func(t *testing.T) {
		t.Parallel()
		out := renderString(t, view, output.FormatHuman)

		assert.Contains(t, out, "active", "phase should appear")
		assert.Contains(t, out, "request", "next-action: suggest request command")
		assert.Contains(t, out, "user:alice", "reviewer identity in command")
		// HUMAN format must NOT contain the background-shell wait hint
		assert.NotContains(t, out, "sleep", "human format should not include background-shell hint")
	})

	t.Run("agent", func(t *testing.T) {
		t.Parallel()
		out := renderString(t, view, output.FormatAgent)

		assert.Contains(t, out, "active", "phase should appear")
		assert.Contains(t, out, "request", "next-action: suggest request command")
		assert.Contains(t, out, "user:alice", "reviewer identity in command")
		// AGENT format MUST contain the background-shell wait hint
		assert.Contains(t, out, "sleep", "agent format should include background-shell hint")
		assert.Contains(t, out, "status", "agent format should reference status command")
	})
}

// ── Active + blocked reviewer ─────────────────────────────────────────────────

func TestRender_Active_Blocked(t *testing.T) {
	t.Parallel()

	blockReason := "no new commit since last review"
	view := output.LoopView{
		Reviewers: []output.ReviewerView{
			{
				Identity:     aliceIdentity(),
				Goal:         reviewloop.GoalApproved,
				Phase:        reviewloop.PhaseActive,
				RallyCount:   1,
				MaxRallies:   3,
				GoalMet:      false,
				CanRerequest: false,
				BlockReason:  blockReason,
			},
		},
	}

	for _, f := range []output.Format{output.FormatHuman, output.FormatAgent} {
		t.Run(string(f), func(t *testing.T) {
			t.Parallel()
			out := renderString(t, view, f)

			assert.Contains(t, out, "active", "phase should appear")
			assert.Contains(t, out, blockReason, "block reason should appear")
			assert.Contains(t, out, "push", "next-action: instruct to push a new commit")
		})
	}
}

// ── Reviewer with unresolved + new comments ───────────────────────────────────

func TestRender_WithComments(t *testing.T) {
	t.Parallel()

	view := output.LoopView{
		Reviewers: []output.ReviewerView{
			{
				Identity:     aliceIdentity(),
				Goal:         reviewloop.GoalAllConversationsResolved,
				Phase:        reviewloop.PhaseActive,
				RallyCount:   1,
				MaxRallies:   3,
				GoalMet:      false,
				CanRerequest: false,
				BlockReason:  "no new commit since last review",
				UnresolvedComments: []output.CommentView{
					{
						Author: "alice",
						Body:   "This looks wrong here.",
						URL:    "https://example.com/c/1",
						At:     time.Now(),
					},
				},
				NewComments: []output.CommentView{
					{
						Author: "alice",
						Body:   "Please fix the nit.",
						URL:    "https://example.com/c/2",
						At:     time.Now(),
					},
				},
			},
		},
	}

	for _, f := range []output.Format{output.FormatHuman, output.FormatAgent} {
		t.Run(string(f), func(t *testing.T) {
			t.Parallel()
			out := renderString(t, view, f)

			// Both comment bodies must appear
			assert.Contains(t, out, "This looks wrong here.", "unresolved comment body should appear")
			assert.Contains(t, out, "Please fix the nit.", "new comment body should appear")
			// next-action always present
			assert.Contains(t, out, "push", "next-action must be present")
		})
	}
}

// ── Done loop ─────────────────────────────────────────────────────────────────

func TestRender_DoneLoop(t *testing.T) {
	t.Parallel()

	view := output.LoopView{
		Reviewers: []output.ReviewerView{
			{
				Identity:   aliceIdentity(),
				Goal:       reviewloop.GoalApproved,
				Phase:      reviewloop.PhaseGoalMet,
				RallyCount: 1,
				MaxRallies: 3,
				GoalMet:    true,
			},
		},
		Done: true,
	}

	for _, f := range []output.Format{output.FormatHuman, output.FormatAgent} {
		t.Run(string(f), func(t *testing.T) {
			t.Parallel()
			out := renderString(t, view, f)

			// Loop completion line must be present
			assert.Contains(t, out, "Loop complete", "done loop should emit completion line")
		})
	}
}

// ── Done loop with exhausted reviewer ─────────────────────────────────────────

func TestRender_DoneLoop_WithExhausted(t *testing.T) {
	t.Parallel()

	bobID := reviewloop.ReviewerIdentity{
		Type: reviewloop.ReviewerTypeUser,
		Name: "bob",
	}
	view := output.LoopView{
		Reviewers: []output.ReviewerView{
			{
				Identity:   aliceIdentity(),
				Goal:       reviewloop.GoalApproved,
				Phase:      reviewloop.PhaseGoalMet,
				RallyCount: 1,
				MaxRallies: 3,
				GoalMet:    true,
			},
			{
				Identity:   bobID,
				Goal:       reviewloop.GoalApproved,
				Phase:      reviewloop.PhaseExhausted,
				RallyCount: 3,
				MaxRallies: 3,
				GoalMet:    false,
			},
		},
		Done: true,
	}

	for _, f := range []output.Format{output.FormatHuman, output.FormatAgent} {
		t.Run(string(f), func(t *testing.T) {
			t.Parallel()
			out := renderString(t, view, f)

			assert.Contains(t, out, "Loop complete", "done loop should emit completion line")
			assert.Contains(t, out, "bob", "exhausted reviewer should be mentioned")
		})
	}
}

// ── Reviewer ordering is stable (slice order) ─────────────────────────────────

func TestRender_StableOrder(t *testing.T) {
	t.Parallel()

	aliceID := reviewloop.ReviewerIdentity{Type: reviewloop.ReviewerTypeUser, Name: "alice"}
	bobID := reviewloop.ReviewerIdentity{Type: reviewloop.ReviewerTypeUser, Name: "bob"}

	view := output.LoopView{
		Reviewers: []output.ReviewerView{
			{
				Identity:   aliceID,
				Goal:       reviewloop.GoalApproved,
				Phase:      reviewloop.PhaseGoalMet,
				RallyCount: 1,
				MaxRallies: 3,
				GoalMet:    true,
			},
			{
				Identity:   bobID,
				Goal:       reviewloop.GoalApproved,
				Phase:      reviewloop.PhaseGoalMet,
				RallyCount: 2,
				MaxRallies: 3,
				GoalMet:    true,
			},
		},
	}

	for _, f := range []output.Format{output.FormatHuman, output.FormatAgent} {
		t.Run(string(f), func(t *testing.T) {
			t.Parallel()
			out := renderString(t, view, f)
			alicePos := strings.Index(out, "alice")
			bobPos := strings.Index(out, "bob")
			assert.Less(t, alicePos, bobPos, "alice (first in slice) should appear before bob")
		})
	}
}
