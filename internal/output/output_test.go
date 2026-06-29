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

func renderString(t *testing.T, v output.LoopView) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, output.Render(&sb, v, ""))
	return sb.String()
}

func renderCheckString(t *testing.T, r core.CheckResult, loopView *output.LoopView) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, output.RenderCheckResult(&sb, r, loopView, ""))
	return sb.String()
}

func aliceIdentity() reviewer.Identity {
	return reviewer.Identity{Type: reviewer.ReviewerTypeUser, Name: "alice"}
}

// ── Reviewer loop rendering ───────────────────────────────────────────────────

func TestRender_GoalMet(t *testing.T) {
	t.Parallel()

	out := renderString(t, output.LoopView{Reviewers: []output.ReviewerView{{
		Identity: aliceIdentity(), Goal: reviewer.GoalApproved,
		Phase: reviewer.PhaseGoalMet, RallyCount: 1, MaxRallies: 3, GoalMet: true,
	}}})

	assert.Contains(t, out, "goal-met", "phase should appear")
	assert.Contains(t, out, "alice", "reviewer name should appear")
	assert.Contains(t, out, "1/3", "rally count should appear")
	assert.Contains(t, out, "Goal met", "next-action: goal met message")
}

func TestRender_Exhausted(t *testing.T) {
	t.Parallel()

	out := renderString(t, output.LoopView{Reviewers: []output.ReviewerView{{
		Identity: aliceIdentity(), Goal: reviewer.GoalApproved,
		Phase: reviewer.PhaseExhausted, RallyCount: 3, MaxRallies: 3,
	}}})

	assert.Contains(t, out, "exhausted", "phase should appear")
	assert.Contains(t, out, "3/3", "rally count should appear")
	assert.Contains(t, out, "WARNING", "next-action: exhausted warning")
	assert.Contains(t, out, "max-rallies", "next-action: suggest raising max-rallies")
}

func TestRender_Active_CanRerequest(t *testing.T) {
	t.Parallel()

	out := renderString(t, output.LoopView{Reviewers: []output.ReviewerView{{
		Identity: aliceIdentity(), Goal: reviewer.GoalApproved,
		Phase: reviewer.PhaseActive, RallyCount: 1, MaxRallies: 3, CanRerequest: true,
	}}})

	assert.Contains(t, out, "active", "phase should appear")
	assert.Contains(t, out, "request", "next-action: suggest request command")
	assert.Contains(t, out, "user:alice", "reviewer identity in command")
	assert.Contains(t, out, "sleep", "next-action includes a non-blocking wait hint")
	assert.Contains(t, out, "check", "next-action references the check command")
}

func TestRender_Active_Blocked(t *testing.T) {
	t.Parallel()

	const blockReason = "no new commit since last review"
	out := renderString(t, output.LoopView{Reviewers: []output.ReviewerView{{
		Identity: aliceIdentity(), Goal: reviewer.GoalApproved,
		Phase: reviewer.PhaseActive, RallyCount: 1, MaxRallies: 3, BlockReason: blockReason,
	}}})

	assert.Contains(t, out, "active", "phase should appear")
	assert.Contains(t, out, blockReason, "block reason should appear")
	assert.Contains(t, out, "Push a new commit", "next-action: instruct to push a new commit")
}

func TestRender_FullMode_ShowsCommentBodies(t *testing.T) {
	t.Parallel()

	out := renderString(t, output.LoopView{Reviewers: []output.ReviewerView{{
		Identity: aliceIdentity(), Goal: reviewer.GoalReviewedClean,
		Phase: reviewer.PhaseActive, RallyCount: 1, MaxRallies: 3,
		UnresolvedComments: []output.CommentView{
			{Author: "alice", Body: "This looks wrong here.", URL: "https://example.com/c/1", At: time.Now()},
		},
	}}})

	assert.Contains(t, out, "This looks wrong here.", "full mode renders comment bodies")
}

func TestRender_Active_WithUnresolved_NextActionIsResolveNotRerequest(t *testing.T) {
	t.Parallel()

	out := renderString(t, output.LoopView{Reviewers: []output.ReviewerView{{
		Identity: aliceIdentity(), Goal: reviewer.GoalReviewedClean,
		Phase: reviewer.PhaseActive, RallyCount: 1, MaxRallies: 5, CanRerequest: true,
		UnresolvedCount: 2,
	}}})

	// With unresolved conversations, the next action must be to resolve them —
	// NOT to re-request (re-requesting does not advance the goal while open).
	assert.Contains(t, out, "Resolve the 2 unresolved")
	assert.NotContains(t, out, "(re)request review", "must not suggest re-request while conversations are open")
}

func TestRender_Active_CanRerequest_PollsInBackground(t *testing.T) {
	t.Parallel()

	out := renderString(t, output.LoopView{Reviewers: []output.ReviewerView{{
		Identity: aliceIdentity(), Goal: reviewer.GoalApproved,
		Phase: reviewer.PhaseActive, RallyCount: 1, MaxRallies: 3, CanRerequest: true,
	}}})

	// The poll-wait must be explicitly a background job (a foreground sleep blocks the agent).
	assert.Contains(t, out, "BACKGROUND")
	assert.Contains(t, out, "background job")
}

func TestRender_Active_ChangesRequested_CanRerequest(t *testing.T) {
	t.Parallel()

	out := renderString(t, output.LoopView{Reviewers: []output.ReviewerView{{
		Identity: aliceIdentity(), Goal: reviewer.GoalApproved,
		Phase: reviewer.PhaseActive, RallyCount: 1, MaxRallies: 3,
		CanRerequest: true, ChangesRequested: true,
	}}})

	assert.Contains(t, out, "requested changes", "changes-requested action is rendered")
	assert.Contains(t, out, "request --reviewer", "should suggest re-request after addressing")
	assert.Contains(t, out, "BACKGROUND", "poll must be a background job")
}

func TestRender_Active_ChangesRequested_Stuck_Escalates(t *testing.T) {
	t.Parallel()

	out := renderString(t, output.LoopView{Reviewers: []output.ReviewerView{{
		Identity: aliceIdentity(), Goal: reviewer.GoalApproved,
		Phase: reviewer.PhaseActive, RallyCount: 1, MaxRallies: 3,
		CanRerequest: false, ChangesRequested: true,
		BlockReason: "no new commit since last review",
	}}})

	// When a re-request cannot advance the loop, the agent must escalate, not spin.
	assert.Contains(t, out, "escalate to a human")
	assert.Contains(t, out, "cannot be")
}

func TestRender_ReviewBody_ConciseMode_PointsAtViewCommand(t *testing.T) {
	t.Parallel()

	out := renderString(t, output.LoopView{Reviewers: []output.ReviewerView{{
		Identity: aliceIdentity(), Goal: reviewer.GoalReviewedClean,
		Phase: reviewer.PhaseGoalMet, GoalMet: true,
		LatestReviewState: reviewer.ReviewStateCommented, LatestReviewCommitOID: "abc1234567",
		ReviewBodyPresent: true, // ReviewBodyDrillInCmd empty → concise mode
	}}})

	assert.Contains(t, out, "view --condition reviewers", "concise mode points at the view command")
	assert.Contains(t, out, "abc1234", "short commit oid is shown")
	assert.NotContains(t, out, "gh api repos", "concise mode does not emit the body drill-in")
}

func TestRender_ReviewBody_FullMode_ShowsDrillIn(t *testing.T) {
	t.Parallel()

	const drillIn = "gh api repos/o/r/pulls/3/reviews/123 --jq .body"
	out := renderString(t, output.LoopView{Reviewers: []output.ReviewerView{{
		Identity: aliceIdentity(), Goal: reviewer.GoalReviewedClean,
		Phase: reviewer.PhaseGoalMet, GoalMet: true,
		LatestReviewState: reviewer.ReviewStateCommented,
		ReviewBodyPresent: true, ReviewBodyDrillInCmd: drillIn,
	}}})

	assert.Contains(t, out, drillIn, "full mode emits the body drill-in command")
}

func TestRender_DoneLoop(t *testing.T) {
	t.Parallel()

	out := renderString(t, output.LoopView{
		Done: true,
		Reviewers: []output.ReviewerView{{
			Identity: aliceIdentity(), Goal: reviewer.GoalApproved,
			Phase: reviewer.PhaseGoalMet, RallyCount: 1, MaxRallies: 3, GoalMet: true,
		}},
	})

	assert.Contains(t, out, "Loop complete", "done loop emits completion line")
}

func TestRender_DoneLoop_WithExhausted(t *testing.T) {
	t.Parallel()

	out := renderString(t, output.LoopView{
		Done: true,
		Reviewers: []output.ReviewerView{
			{Identity: aliceIdentity(), Goal: reviewer.GoalApproved, Phase: reviewer.PhaseGoalMet, GoalMet: true},
			{
				Identity: reviewer.Identity{Type: reviewer.ReviewerTypeUser, Name: "bob"},
				Goal:     reviewer.GoalApproved, Phase: reviewer.PhaseExhausted, RallyCount: 3, MaxRallies: 3,
			},
		},
	})

	assert.Contains(t, out, "Loop complete", "done loop emits completion line")
	assert.Contains(t, out, "bob", "exhausted reviewer should be named in the warning")
}

func TestRender_StableOrder(t *testing.T) {
	t.Parallel()

	out := renderString(t, output.LoopView{Reviewers: []output.ReviewerView{
		{
			Identity: reviewer.Identity{Type: reviewer.ReviewerTypeUser, Name: "alice"},
			Phase:    reviewer.PhaseGoalMet,
			GoalMet:  true,
		},
		{
			Identity: reviewer.Identity{Type: reviewer.ReviewerTypeUser, Name: "bob"},
			Phase:    reviewer.PhaseGoalMet,
			GoalMet:  true,
		},
	}})

	assert.Less(t, strings.Index(out, "alice"), strings.Index(out, "bob"),
		"alice (first in slice) should appear before bob")
}

func TestRender_MarkdownHierarchy(t *testing.T) {
	t.Parallel()

	out := renderString(t, output.LoopView{Reviewers: []output.ReviewerView{{
		Identity: aliceIdentity(), Goal: reviewer.GoalApproved,
		Phase: reviewer.PhaseGoalMet, GoalMet: true,
	}}})

	// The loop is an H2 section; each reviewer is an H3 under it — never the same level.
	assert.Contains(t, out, "## Reviewer loop", "loop section is H2")
	assert.Contains(t, out, "### user:alice", "each reviewer is H3")
}

// ── RenderCheckResult ─────────────────────────────────────────────────────────

func TestRenderCheckResult_StatusLine(t *testing.T) {
	t.Parallel()

	assert.Contains(t, renderCheckString(t, core.CheckResult{Satisfied: true}, nil), "status: satisfied")

	blocked := core.CheckResult{Blockers: []core.Condition{
		{Kind: core.ConditionConflict, Severity: core.SeverityBlocker, Title: "Merge conflicts"},
	}}
	assert.Contains(t, renderCheckString(t, blocked, nil), "status: blocked")
}

func TestRenderCheckResult_Satisfied_AllDimensionsChecked(t *testing.T) {
	t.Parallel()

	// With no blockers, every dimension renders as a checked task item.
	out := renderCheckString(t, core.CheckResult{Satisfied: true}, nil)

	assert.Contains(t, out, "- [x] conflicts")
	assert.Contains(t, out, "- [x] base up-to-date")
	assert.Contains(t, out, "- [x] required checks")
}

func TestRenderCheckResult_Blockers_AsTaskItems(t *testing.T) {
	t.Parallel()

	out := renderCheckString(t, core.CheckResult{Blockers: []core.Condition{{
		Kind: core.ConditionCheckFailing, Severity: core.SeverityBlocker,
		Title: "Required CI check failing", Detail: "build / lint",
	}}}, nil)

	// A failing required check is an unchecked item with an indented status
	// detail naming the check and a "→ action" pointing at `view`; the other
	// dimensions stay checked.
	assert.Contains(t, out, "- [ ] required checks")
	assert.Contains(t, out, "build / lint failing", "indented status names the failing check")
	assert.Contains(t, out, "→ fix the failing required checks")
	assert.Contains(t, out, "view --condition checks")
	assert.Contains(t, out, "- [x] conflicts")
	assert.NotContains(t, out, "## Blockers", "the task list replaces the section format")
}

func TestRenderCheckResult_Advisories_AsTildeLines(t *testing.T) {
	t.Parallel()

	out := renderCheckString(t, core.CheckResult{
		Satisfied: true,
		Advisories: []core.Condition{{
			Kind: core.ConditionApprovalRequired, Severity: core.SeverityAdvisory,
			Title: "Human approval required", Detail: "reviewDecision=REVIEW_REQUIRED",
		}},
	}, nil)

	assert.Contains(t, out, "status: satisfied")
	assert.Contains(t, out, "~ approval required (human)", "advisory is a trailing ~ line, shown even when satisfied")
}

func TestRenderCheckResult_Reviewer_AsTaskItems(t *testing.T) {
	t.Parallel()

	loopView := &output.LoopView{
		Reviewers: []output.ReviewerView{
			{Identity: aliceIdentity(), Phase: reviewer.PhaseGoalMet, GoalMet: true},
			{
				Identity: reviewer.Identity{Type: reviewer.ReviewerTypeGitHubApp, Name: "coderabbitai"},
				Phase:    reviewer.PhaseActive, RallyCount: 3, MaxRallies: 5, UnresolvedCount: 2,
			},
		},
	}
	out := renderCheckString(t, core.CheckResult{}, loopView)

	assert.Contains(t, out, "- [x] user:alice — goal met")
	assert.Contains(t, out, "- [ ] github-app:coderabbitai — 2 unresolved")
	assert.Contains(t, out, "rally 3/5", "rally is an indented supplement under the item")
	assert.NotContains(t, out, "## Reviewer loop", "reviewers are task items, not a detailed section")
}

func TestRenderCheckResult_ReviewBody_TildeLine(t *testing.T) {
	t.Parallel()

	loopView := &output.LoopView{Reviewers: []output.ReviewerView{{
		Identity: aliceIdentity(), Phase: reviewer.PhaseGoalMet, GoalMet: true,
		LatestReviewState: reviewer.ReviewStateCommented, ReviewBodyPresent: true,
	}}}
	out := renderCheckString(t, core.CheckResult{Satisfied: true}, loopView)

	assert.Contains(t, out, "~ review notes present — mergeable-please view --condition reviewers")
}

func TestRenderCheckResult_ChangesRequested_NextEscalatesOrAddresses(t *testing.T) {
	t.Parallel()

	loopView := &output.LoopView{Reviewers: []output.ReviewerView{{
		Identity: reviewer.Identity{Type: reviewer.ReviewerTypeGitHubApp, Name: "coderabbitai"},
		Phase:    reviewer.PhaseActive, RallyCount: 1, MaxRallies: 5, ChangesRequested: true,
	}}}
	out := renderCheckString(t, core.CheckResult{}, loopView)

	assert.Contains(t, out, "- [ ] github-app:coderabbitai — changes requested")
	assert.Contains(t, out, "rally 1/5", "rally is an indented supplement")
	assert.Contains(t, out, "escalate", "the indented action says to address or escalate")
}

func TestRenderCheckResult_Exhausted_DoneWithWarning(t *testing.T) {
	t.Parallel()

	loopView := &output.LoopView{Reviewers: []output.ReviewerView{{
		Identity: aliceIdentity(), Phase: reviewer.PhaseExhausted, RallyCount: 5, MaxRallies: 5,
	}}}
	out := renderCheckString(t, core.CheckResult{Satisfied: true}, loopView)

	assert.Contains(t, out, "- [x] user:alice — exhausted (5/5) ⚠", "exhausted is terminal/done with a warning marker")
	assert.Contains(t, out, "goal not met after max rallies", "the warning is an indented detail under the item")
}

// ── RenderDimensionView ───────────────────────────────────────────────────────

func TestRenderDimensionView_NoStatusLine(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	blockers := []core.Condition{{Kind: core.ConditionCheckFailing, Title: "Required CI check failing"}}
	require.NoError(t, output.RenderDimensionView(&sb, blockers, nil, ""))
	out := sb.String()

	assert.NotContains(t, out, "status:", "dimension view must not emit a global status verdict")
	assert.Contains(t, out, "check-failing")
}

func TestRenderCheckResult_TargetInHeader(t *testing.T) {
	t.Parallel()

	const target = "o/r#3 https://github.com/o/r/pull/3"
	var sb strings.Builder
	require.NoError(t, output.RenderCheckResult(&sb, core.CheckResult{Satisfied: true}, nil, target))
	out := sb.String()

	// status and target share the first line: "status: satisfied · <target>".
	assert.Contains(t, out, "status: satisfied · "+target, "header identifies status and the inspected PR")
}

func TestRenderDimensionView_TargetLine(t *testing.T) {
	t.Parallel()

	const target = "o/r#3 https://github.com/o/r/pull/3"
	var sb strings.Builder
	require.NoError(t, output.RenderDimensionView(&sb, nil, nil, target))
	assert.Contains(t, sb.String(), "target: "+target)
}

func TestRenderDimensionView_Empty(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	require.NoError(t, output.RenderDimensionView(&sb, nil, nil, ""))
	assert.Contains(t, sb.String(), "No conditions found")
}
