package core

import "github.com/sushichan044/mergeable-please/internal/core/reviewer"

// CheckResult is the aggregate merge-readiness evaluation returned by a Backend.
// Blockers gate the Satisfied state; Advisories are always shown but never block.
type CheckResult struct {
	// Satisfied is true when there are no Blockers and the reviewer loop (if any)
	// is fully terminal. Set by calling Finalize.
	Satisfied bool

	// Blockers are conditions that prevent merging and that an AI agent can address.
	Blockers []Condition

	// Advisories are conditions shown for awareness but that don't block merging.
	// These typically require human action (approval, deployment gates).
	Advisories []Condition

	// ReviewerLoop holds the aggregate reviewer-loop state when reviewers are
	// configured. nil means no reviewer loop was evaluated.
	ReviewerLoop *reviewer.LoopState

	// HeadCommitOID is the head commit SHA at query time.
	HeadCommitOID string

	// MergeStateRaw is the raw mergeStateStatus value from GitHub, for debugging.
	MergeStateRaw string
}

// Finalize computes Satisfied from the current Blockers and ReviewerLoop state.
// Call this after all conditions and the reviewer loop have been populated.
//
// Satisfied = (no Blockers) AND (ReviewerLoop == nil OR loop is Done).
// Advisories never affect Satisfied.
func (r *CheckResult) Finalize() {
	loopDone := r.ReviewerLoop == nil || r.ReviewerLoop.Done
	r.Satisfied = len(r.Blockers) == 0 && loopDone
}
