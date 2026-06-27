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

// isEngaged returns true when the reviewer has at least one non-pending review
// or at least one thread attributed to them. An unengaged reviewer cannot have
// GoalAllConversationsResolved declared vacuously met.
func isEngaged(identity Identity, s Snapshot) bool {
	if _, ok := latestNonPendingReview(identity, s.Reviews); ok {
		return true
	}
	for _, t := range s.Threads {
		if identityMatches(t.Reviewer, identity) {
			return true
		}
	}
	return false
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

// latestReviewIsChangesRequested reports whether the reviewer's latest
// non-pending review requested changes. A changes-requested review is sticky on
// GitHub: it keeps blocking until the reviewer submits another review with a
// different state, regardless of new commits.
func latestReviewIsChangesRequested(identity Identity, s Snapshot) bool {
	latest, ok := latestNonPendingReview(identity, s.Reviews)
	return ok && latest.State == ReviewStateChangesRequested
}

// goalMet evaluates whether the goal defined in policy p is satisfied.
func goalMet(p Policy, s Snapshot) bool {
	switch p.Goal {
	case GoalApproved:
		latest, ok := latestNonPendingReview(p.Identity, s.Reviews)
		if !ok {
			return false
		}
		// Require approval on the current head — an approval on an older commit
		// is stale (reviewer returns to active after new commits).
		return latest.State == ReviewStateApproved && latest.CommitOID == s.HeadCommitOID

	case GoalAllConversationsResolved:
		// An unengaged reviewer cannot have their goal declared met vacuously:
		// they must first submit a review or leave a thread, or the initial
		// review request would never be fired.
		if !isEngaged(p.Identity, s) {
			return false
		}
		// An active changes-requested review gates the goal even when every
		// inline thread is resolved: the reviewer is still formally blocking.
		if latestReviewIsChangesRequested(p.Identity, s) {
			return false
		}
		for _, t := range s.Threads {
			if identityMatches(t.Reviewer, p.Identity) && !t.Resolved {
				return false
			}
		}
		return true

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
