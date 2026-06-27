package output_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sushichan044/mergeable-please/internal/core"
	"github.com/sushichan044/mergeable-please/internal/core/reviewer"
	"github.com/sushichan044/mergeable-please/internal/output"
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

func aliceIdentity() reviewer.Identity {
	return reviewer.Identity{
		Type: reviewer.ReviewerTypeUser,
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
				Goal:       reviewer.GoalApproved,
				Phase:      reviewer.PhaseGoalMet,
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
				Goal:       reviewer.GoalApproved,
				Phase:      reviewer.PhaseExhausted,
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
				Goal:         reviewer.GoalApproved,
				Phase:        reviewer.PhaseActive,
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
		assert.Contains(t, out, "check", "agent format should reference check command")
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
				Goal:         reviewer.GoalApproved,
				Phase:        reviewer.PhaseActive,
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
				Goal:         reviewer.GoalAllConversationsResolved,
				Phase:        reviewer.PhaseActive,
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
				Goal:       reviewer.GoalApproved,
				Phase:      reviewer.PhaseGoalMet,
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

	bobID := reviewer.Identity{
		Type: reviewer.ReviewerTypeUser,
		Name: "bob",
	}
	view := output.LoopView{
		Reviewers: []output.ReviewerView{
			{
				Identity:   aliceIdentity(),
				Goal:       reviewer.GoalApproved,
				Phase:      reviewer.PhaseGoalMet,
				RallyCount: 1,
				MaxRallies: 3,
				GoalMet:    true,
			},
			{
				Identity:   bobID,
				Goal:       reviewer.GoalApproved,
				Phase:      reviewer.PhaseExhausted,
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

	aliceID := reviewer.Identity{Type: reviewer.ReviewerTypeUser, Name: "alice"}
	bobID := reviewer.Identity{Type: reviewer.ReviewerTypeUser, Name: "bob"}

	view := output.LoopView{
		Reviewers: []output.ReviewerView{
			{
				Identity:   aliceID,
				Goal:       reviewer.GoalApproved,
				Phase:      reviewer.PhaseGoalMet,
				RallyCount: 1,
				MaxRallies: 3,
				GoalMet:    true,
			},
			{
				Identity:   bobID,
				Goal:       reviewer.GoalApproved,
				Phase:      reviewer.PhaseGoalMet,
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

// ── RenderCheckResult ─────────────────────────────────────────────────────────

func renderCheckString(t *testing.T, r core.CheckResult, loopView *output.LoopView, f output.Format) string {
	t.Helper()
	var sb strings.Builder
	err := output.RenderCheckResult(&sb, r, loopView, f)
	require.NoError(t, err)
	return sb.String()
}

func TestRenderCheckResult_Satisfied_EmitsStatusLine(t *testing.T) {
	t.Parallel()

	r := core.CheckResult{Satisfied: true}

	for _, f := range []output.Format{output.FormatHuman, output.FormatAgent} {
		t.Run(string(f), func(t *testing.T) {
			t.Parallel()
			out := renderCheckString(t, r, nil, f)
			assert.Contains(t, out, "status: satisfied")
		})
	}
}

func TestRenderCheckResult_Blocked_EmitsStatusLine(t *testing.T) {
	t.Parallel()

	r := core.CheckResult{
		Satisfied: false,
		Blockers: []core.Condition{
			{Kind: core.ConditionConflict, Severity: core.SeverityBlocker, Title: "Merge conflicts"},
		},
	}

	for _, f := range []output.Format{output.FormatHuman, output.FormatAgent} {
		t.Run(string(f), func(t *testing.T) {
			t.Parallel()
			out := renderCheckString(t, r, nil, f)
			assert.Contains(t, out, "status: blocked")
		})
	}
}

func TestRenderCheckResult_Blockers_Shown(t *testing.T) {
	t.Parallel()

	r := core.CheckResult{
		Satisfied: false,
		Blockers: []core.Condition{
			{
				Kind:            core.ConditionCheckFailing,
				Severity:        core.SeverityBlocker,
				Title:           "Required CI check failing",
				Detail:          "build / lint",
				SuggestedAction: "Fix lint errors and push.",
				DrillInCmd:      "mergeable-please view --condition checks",
			},
		},
	}

	for _, f := range []output.Format{output.FormatHuman, output.FormatAgent} {
		t.Run(string(f), func(t *testing.T) {
			t.Parallel()
			out := renderCheckString(t, r, nil, f)
			assert.Contains(t, out, "check-failing")
			assert.Contains(t, out, "build / lint")
		})
	}
}

func TestRenderCheckResult_Advisories_AlwaysShown(t *testing.T) {
	t.Parallel()

	// Satisfied with only an advisory — advisory must still appear.
	r := core.CheckResult{
		Satisfied: true,
		Advisories: []core.Condition{
			{
				Kind:     core.ConditionApprovalRequired,
				Severity: core.SeverityAdvisory,
				Title:    "Human approval required",
				Detail:   "reviewDecision=REVIEW_REQUIRED",
			},
		},
	}

	for _, f := range []output.Format{output.FormatHuman, output.FormatAgent} {
		t.Run(string(f), func(t *testing.T) {
			t.Parallel()
			out := renderCheckString(t, r, nil, f)
			assert.Contains(t, out, "status: satisfied", "satisfied with advisory only")
			assert.Contains(t, out, "approval-required", "advisory must appear even when satisfied")
		})
	}
}

func TestRenderCheckResult_ReviewerLoop_DetailRendered(t *testing.T) {
	t.Parallel()

	loopView := &output.LoopView{
		Done: true,
		Reviewers: []output.ReviewerView{
			{
				Identity:   aliceIdentity(),
				Goal:       reviewer.GoalApproved,
				Phase:      reviewer.PhaseGoalMet,
				RallyCount: 1,
				MaxRallies: 3,
				GoalMet:    true,
			},
		},
	}
	r := core.CheckResult{Satisfied: true}

	for _, f := range []output.Format{output.FormatHuman, output.FormatAgent} {
		t.Run(string(f), func(t *testing.T) {
			t.Parallel()
			out := renderCheckString(t, r, loopView, f)
			assert.Contains(t, out, "Reviewer loop", "reviewer loop header must appear")
			// The fix: per-reviewer detail must render, not just the header.
			assert.Contains(t, out, "alice", "reviewer identity must be rendered")
			assert.Contains(t, out, "goal-met", "reviewer phase must be rendered")
		})
	}
}
