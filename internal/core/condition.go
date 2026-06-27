// Package core defines the backend-agnostic merge-readiness model used across
// the mergeable-please tool.
package core

// Severity classifies whether a condition blocks the "done" determination.
type Severity string

const (
	// SeverityBlocker means the condition prevents Satisfied from being true.
	// AI agents can potentially resolve blockers.
	SeverityBlocker Severity = "blocker"

	// SeverityAdvisory means the condition is always shown but never prevents Satisfied.
	// Advisories represent human-only resolutions (approval, deployment gates, etc.).
	SeverityAdvisory Severity = "advisory"
)

// ConditionKind identifies what kind of condition is reported.
type ConditionKind string

const (
	// ConditionConflict is a blocker: the branch has merge conflicts.
	ConditionConflict ConditionKind = "conflict"

	// ConditionBehindBase is a blocker: the branch is behind its base and the
	// repository enforces up-to-date branches (mergeStateStatus=BEHIND appeared).
	ConditionBehindBase ConditionKind = "behind-base"

	// ConditionCheckFailing is a blocker: one or more required CI checks are failing.
	ConditionCheckFailing ConditionKind = "check-failing"

	// ConditionCheckPending is a blocker: one or more required CI checks are still running.
	ConditionCheckPending ConditionKind = "check-pending"

	// ConditionApprovalRequired is an advisory: human review approval is required.
	// reviewDecision=REVIEW_REQUIRED. AI cannot satisfy this.
	ConditionApprovalRequired ConditionKind = "approval-required"

	// ConditionChangesRequested is an advisory: at least one reviewer requested changes.
	// Configured reviewers are handled by the reviewer loop; unconfigured ones appear here.
	ConditionChangesRequested ConditionKind = "changes-requested"

	// ConditionResidualRuleset is an advisory: BLOCKED could not be attributed to any
	// known field (e.g. signed commits required, linear history, deployment gates).
	// Use `view --condition rules` for the ruleset landscape.
	ConditionResidualRuleset ConditionKind = "residual-ruleset"

	// ConditionMergeEligibilityPending is a blocker: mergeable/mergeStateStatus is UNKNOWN —
	// GitHub is still computing merge eligibility. Re-run in 15-30 seconds.
	ConditionMergeEligibilityPending ConditionKind = "merge-eligibility-pending"

	// ConditionCheckTruncated is an advisory: GitHub returned more than 100 status checks
	// and the list was truncated. Run `gh pr checks <n>` for the full list.
	ConditionCheckTruncated ConditionKind = "check-truncated"
)

// Condition is a single merge-readiness finding.
type Condition struct {
	Kind            ConditionKind
	Severity        Severity
	Title           string
	Detail          string
	SuggestedAction string
	DrillInCmd      string
}
