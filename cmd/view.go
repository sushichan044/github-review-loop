package cmd

import (
	"github.com/sushichan044/github-review-loop/internal/output"
	"github.com/sushichan044/github-review-loop/internal/reviewloop"
)

// buildLoopView maps a [reviewloop.LoopState] and its corresponding policies into an
// [output.LoopView], merging in the per-reviewer unresolved comments from unresolvedByKey.
//
// unresolvedByKey is keyed by "type:name" (or "type" when name is empty),
// matching the format returned by [github.UnresolvedThreadComments].
//
// NOTE: NewComments is left empty in v1. Populating it requires per-comment timestamps
// and correlation with rally timing, which are not yet tracked in the thread fetch.
func buildLoopView(
	state reviewloop.LoopState,
	policies []reviewloop.Policy,
	unresolvedByKey map[string][]output.CommentView,
) output.LoopView {
	policyByIdentity := make(map[reviewloop.ReviewerIdentity]reviewloop.Policy, len(policies))
	for _, p := range policies {
		policyByIdentity[p.Identity] = p
	}

	reviewerViews := make([]output.ReviewerView, 0, len(state.Reviewers))

	for _, rs := range state.Reviewers {
		p := policyByIdentity[rs.Identity]
		key := identityKeyFromReviewerIdentity(rs.Identity)

		reviewerViews = append(reviewerViews, output.ReviewerView{
			Identity:     rs.Identity,
			Goal:         p.Goal,
			Phase:        rs.Phase,
			RallyCount:   rs.RallyCount,
			MaxRallies:   p.MaxRallies,
			GoalMet:      rs.GoalMet,
			CanRerequest: rs.CanRerequest,
			BlockReason:  rs.BlockReason,
			// Populated from the unresolved thread comments accessor.
			UnresolvedComments: unresolvedByKey[key],
			// v1 limitation: NewComments requires per-comment timestamps + rally correlation.
			NewComments: nil,
		})
	}

	return output.LoopView{
		Reviewers: reviewerViews,
		Done:      state.Done,
	}
}

// identityKeyFromReviewerIdentity returns the canonical "type:name" key string
// matching [github.identityKey] without importing the github package.
func identityKeyFromReviewerIdentity(id reviewloop.ReviewerIdentity) string {
	if id.Name == "" {
		return string(id.Type)
	}

	return string(id.Type) + ":" + id.Name
}
