package core_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sushichan044/mergeable-please/internal/core"
)

func TestSeverityValues(t *testing.T) {
	t.Parallel()

	assert.Equal(t, core.SeverityBlocker, core.Severity("blocker"))
	assert.Equal(t, core.SeverityAdvisory, core.Severity("advisory"))
}

func TestConditionKindValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind core.ConditionKind
		want string
	}{
		{"conflict", core.ConditionConflict, "conflict"},
		{"behind-base", core.ConditionBehindBase, "behind-base"},
		{"check-failing", core.ConditionCheckFailing, "check-failing"},
		{"check-pending", core.ConditionCheckPending, "check-pending"},
		{"approval-required", core.ConditionApprovalRequired, "approval-required"},
		{"changes-requested", core.ConditionChangesRequested, "changes-requested"},
		{"residual-ruleset", core.ConditionResidualRuleset, "residual-ruleset"},
		{"merge-eligibility-pending", core.ConditionMergeEligibilityPending, "merge-eligibility-pending"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, string(tc.kind))
		})
	}
}

func TestConditionFields(t *testing.T) {
	t.Parallel()

	c := core.Condition{
		Kind:            core.ConditionCheckFailing,
		Severity:        core.SeverityBlocker,
		Title:           "Required CI check is failing",
		Detail:          "build / lint",
		SuggestedAction: "Fix the lint errors and push a new commit.",
		DrillInCmd:      "mergeable-please view --condition checks",
	}

	assert.Equal(t, core.ConditionCheckFailing, c.Kind)
	assert.Equal(t, core.SeverityBlocker, c.Severity)
	assert.Equal(t, "Required CI check is failing", c.Title)
	assert.Equal(t, "build / lint", c.Detail)
	assert.NotEmpty(t, c.SuggestedAction)
	assert.NotEmpty(t, c.DrillInCmd)
}
