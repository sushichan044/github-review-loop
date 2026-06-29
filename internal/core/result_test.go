package core_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sushichan044/mergeable-please/internal/core"
	"github.com/sushichan044/mergeable-please/internal/core/reviewer"
)

func TestCheckResult_Finalize_NoBlockers_NoLoop_IsSatisfied(t *testing.T) {
	t.Parallel()

	r := core.CheckResult{}
	r.Finalize()

	assert.True(t, r.Satisfied)
}

func TestCheckResult_Finalize_WithBlockers_IsNotSatisfied(t *testing.T) {
	t.Parallel()

	r := core.CheckResult{
		Blockers: []core.Condition{
			{Kind: core.ConditionConflict, Severity: core.SeverityBlocker},
		},
	}
	r.Finalize()

	assert.False(t, r.Satisfied)
}

func TestCheckResult_Finalize_AdvisoriesOnly_IsSatisfied(t *testing.T) {
	t.Parallel()

	r := core.CheckResult{
		Advisories: []core.Condition{
			{Kind: core.ConditionApprovalRequired, Severity: core.SeverityAdvisory},
		},
	}
	r.Finalize()

	// Advisories alone never block.
	assert.True(t, r.Satisfied)
}

func TestCheckResult_Finalize_WithBlockerAndAdvisory_IsNotSatisfied(t *testing.T) {
	t.Parallel()

	r := core.CheckResult{
		Blockers: []core.Condition{
			{Kind: core.ConditionCheckFailing, Severity: core.SeverityBlocker},
		},
		Advisories: []core.Condition{
			{Kind: core.ConditionApprovalRequired, Severity: core.SeverityAdvisory},
		},
	}
	r.Finalize()

	assert.False(t, r.Satisfied)
}

func TestCheckResult_Finalize_LoopNil_IsSatisfied(t *testing.T) {
	t.Parallel()

	r := core.CheckResult{ReviewerLoop: nil}
	r.Finalize()

	assert.True(t, r.Satisfied)
}

func TestCheckResult_Finalize_LoopDone_IsSatisfied(t *testing.T) {
	t.Parallel()

	loop := &reviewer.LoopState{Done: true}
	r := core.CheckResult{ReviewerLoop: loop}
	r.Finalize()

	assert.True(t, r.Satisfied)
}

func TestCheckResult_Finalize_LoopNotDone_IsNotSatisfied(t *testing.T) {
	t.Parallel()

	loop := &reviewer.LoopState{Done: false}
	r := core.CheckResult{ReviewerLoop: loop}
	r.Finalize()

	assert.False(t, r.Satisfied)
}

func TestCheckResult_Finalize_BlockerWithLoopDone_IsNotSatisfied(t *testing.T) {
	t.Parallel()

	loop := &reviewer.LoopState{Done: true}
	r := core.CheckResult{
		Blockers:     []core.Condition{{Kind: core.ConditionConflict, Severity: core.SeverityBlocker}},
		ReviewerLoop: loop,
	}
	r.Finalize()

	// Blocker takes precedence over loop being done.
	assert.False(t, r.Satisfied)
}
