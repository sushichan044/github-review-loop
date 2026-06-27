package reviewer

import "strings"

// identityMatches returns true when a and b represent the same reviewer.
// Name comparison is case-insensitive; Type must match exactly.
func identityMatches(a, b Identity) bool {
	return a.Type == b.Type && strings.EqualFold(a.Name, b.Name)
}

// isTerminal reports whether p is a terminal phase (GoalMet or Exhausted).
func isTerminal(p Phase) bool {
	return p == PhaseGoalMet || p == PhaseExhausted
}

// latestNonPendingReview returns the most recent review for the given identity
// that is not Pending, and a boolean indicating whether one was found.
func latestNonPendingReview(identity Identity, reviews []Review) (Review, bool) {
	var found Review
	var have bool

	for _, r := range reviews {
		if !identityMatches(r.Reviewer, identity) {
			continue
		}
		if r.State == ReviewStatePending {
			continue
		}
		if !have || r.At.After(found.At) {
			found = r
			have = true
		}
	}

	return found, have
}

// latestTrigger returns the most recent TriggerAction for the given identity.
func latestTrigger(identity Identity, triggers []TriggerAction) (TriggerAction, bool) {
	var found TriggerAction
	var have bool

	for _, t := range triggers {
		if !identityMatches(t.Reviewer, identity) {
			continue
		}
		if !have || t.At.After(found.At) {
			found = t
			have = true
		}
	}

	return found, have
}

// hasOutstandingRequest returns true when the reviewer has an unanswered review
// request: a TriggerAction whose At is after their latest non-pending review's
// At, or any TriggerAction when no non-pending review exists.
//
// This prevents firing a second request while the first has not been answered.
func hasOutstandingRequest(identity Identity, s Snapshot) bool {
	latestTrig, hasTrig := latestTrigger(identity, s.Triggers)
	if !hasTrig {
		return false
	}
	latestReview, hasReview := latestNonPendingReview(identity, s.Reviews)
	if !hasReview {
		// Triggered but no non-pending response yet.
		return true
	}
	return latestTrig.At.After(latestReview.At)
}

// rallyCount counts TriggerActions whose Reviewer matches identity.
func rallyCount(identity Identity, triggers []TriggerAction) int {
	count := 0
	for _, t := range triggers {
		if identityMatches(t.Reviewer, identity) {
			count++
		}
	}
	return count
}

// goalMet evaluates whether the goal defined in policy p is satisfied. Both
// goals require the qualifying review to be on the current head: a review of an
// older commit is stale once new commits are pushed.
func goalMet(p Policy, s Snapshot) bool {
	latest, ok := latestNonPendingReview(p.Identity, s.Reviews)
	if !ok || latest.CommitOID != s.HeadCommitOID {
		return false
	}

	switch p.Goal {
	case GoalApproved:
		return latest.State == ReviewStateApproved

	case GoalReviewedClean:
		// The reviewer looked at the current head and signed off: either an
		// approval, or a non-changes-requested review with no inline findings.
		// Resolving threads is NOT enough — the reviewer must re-review cleanly.
		switch latest.State {
		case ReviewStateApproved:
			return true
		case ReviewStateCommented:
			return latest.InlineCommentCount == 0
		case ReviewStateChangesRequested, ReviewStateDismissed, ReviewStatePending:
			return false
		}
		return false

	default:
		return false
	}
}

// rerequestState returns (canRerequest, blockReason) for the given phase and snapshot.
// It is extracted from Evaluate to keep nesting depth manageable.
func rerequestState(p Policy, s Snapshot, phase Phase) (bool, string) {
	if isTerminal(phase) {
		return false, "reviewer is in a terminal phase"
	}

	// Outstanding-request guard: do not fire a second request while the
	// previous one has not yet been answered (avoids spam and wasted API calls).
	if hasOutstandingRequest(p.Identity, s) {
		return false, "review already requested; awaiting response"
	}

	// Engaged reviewer: block re-request if no new commit was pushed since the
	// last review.
	if latest, ok := latestNonPendingReview(p.Identity, s.Reviews); ok {
		if latest.CommitOID == s.HeadCommitOID {
			return false, "no new commit since last review"
		}
	}

	// No prior non-pending review and no outstanding request → allow (initial
	// request).
	return true, ""
}

// Evaluate computes the State for a single policy against the given snapshot.
func Evaluate(p Policy, s Snapshot) State {
	count := rallyCount(p.Identity, s.Triggers)
	met := goalMet(p, s)

	var phase Phase

	switch {
	case met:
		phase = PhaseGoalMet
	case count >= p.MaxRallies:
		phase = PhaseExhausted
	default:
		phase = PhaseActive
	}

	canRerequest, blockReason := rerequestState(p, s, phase)

	latest, hasLatest := latestNonPendingReview(p.Identity, s.Reviews)

	return State{
		Identity:                p.Identity,
		Phase:                   phase,
		RallyCount:              count,
		GoalMet:                 met,
		CanRerequest:            canRerequest,
		BlockReason:             blockReason,
		ChangesRequested:        hasLatest && latest.State == ReviewStateChangesRequested,
		LatestReviewState:       latest.State,
		LatestReviewCommitOID:   latest.CommitOID,
		LatestReviewID:          latest.ID,
		LatestReviewBodyPresent: hasLatest && strings.TrimSpace(latest.Body) != "",
	}
}

// EvaluateLoop evaluates all policies and returns the aggregate loop state.
// Done is true only when every reviewer is in a terminal phase.
//
// When policies is empty, Done is vacuously true (all-of-nothing is trivially
// satisfied). Callers that want to distinguish "no reviewers configured" from
// "all reviewers reached their goal" should guard on len(policies) == 0 before
// trusting Done, as check.go and view.go both do via resolvePolicies.
func EvaluateLoop(policies []Policy, s Snapshot) LoopState {
	reviewers := make([]State, 0, len(policies))
	allDone := true

	for _, p := range policies {
		rs := Evaluate(p, s)
		reviewers = append(reviewers, rs)
		if !isTerminal(rs.Phase) {
			allDone = false
		}
	}

	return LoopState{
		Reviewers: reviewers,
		Done:      allDone,
	}
}
