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

// goalMet evaluates whether the goal defined in policy p is satisfied.
func goalMet(p Policy, s Snapshot) bool {
	switch p.Goal {
	case GoalApproved:
		latest, ok := latestNonPendingReview(p.Identity, s.Reviews)
		if !ok {
			return false
		}
		return latest.State == ReviewStateApproved

	case GoalAllConversationsResolved:
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

	canRerequest := true
	blockReason := ""

	if isTerminal(phase) {
		canRerequest = false
		blockReason = "reviewer is in a terminal phase"
	} else {
		// Active: check whether the head has advanced since the last review.
		if latest, ok := latestNonPendingReview(p.Identity, s.Reviews); ok {
			if latest.CommitOID == s.HeadCommitOID {
				canRerequest = false
				blockReason = "no new commit since last review"
			}
		}
		// No prior non-pending review → allow (initial request).
	}

	return State{
		Identity:     p.Identity,
		Phase:        phase,
		RallyCount:   count,
		GoalMet:      met,
		CanRerequest: canRerequest,
		BlockReason:  blockReason,
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
