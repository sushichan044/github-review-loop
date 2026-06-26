package cmd

import (
	"strings"
	"time"

	"github.com/sushichan044/mergeable-please/internal/backend/github"
	"github.com/sushichan044/mergeable-please/internal/core/reviewer"
	"github.com/sushichan044/mergeable-please/internal/output"
)

// buildLoopView maps a [reviewer.LoopState] and its corresponding policies into an
// [output.LoopView], merging in per-reviewer unresolved and new comments.
//
// allCommentsByKey is keyed by "type:name" (or "type" when name is empty),
// matching the format returned by [github.ThreadComments]. It includes both resolved
// and unresolved comments with their CreatedAt timestamps.
//
// For each reviewer:
//   - UnresolvedComments = comments with Resolved == false
//   - NewComments = comments with CreatedAt.After(lastRally), where lastRally is
//     the latest TriggerAction.At for that reviewer (zero time when no trigger exists,
//     so all comments count as new).
func buildLoopView(
	state reviewer.LoopState,
	snapshot reviewer.Snapshot,
	policies []reviewer.Policy,
	allCommentsByKey map[string][]github.ThreadComment,
) output.LoopView {
	policyByIdentity := make(map[reviewer.Identity]reviewer.Policy, len(policies))
	for _, p := range policies {
		policyByIdentity[p.Identity] = p
	}

	reviewerViews := make([]output.ReviewerView, 0, len(state.Reviewers))

	for _, rs := range state.Reviewers {
		p := policyByIdentity[rs.Identity]
		key := identityKeyFromReviewerIdentity(rs.Identity)

		lastRally := lastRallyTime(rs.Identity, snapshot.Triggers)

		var unresolvedComments []output.CommentView
		var newComments []output.CommentView

		for _, c := range allCommentsByKey[key] {
			cv := output.CommentView{
				Author: c.Author,
				Body:   c.Body,
				URL:    c.URL,
				At:     c.CreatedAt,
			}
			if !c.Resolved {
				unresolvedComments = append(unresolvedComments, cv)
			}
			if c.CreatedAt.After(lastRally) {
				newComments = append(newComments, cv)
			}
		}

		reviewerViews = append(reviewerViews, output.ReviewerView{
			Identity:           rs.Identity,
			Goal:               p.Goal,
			Phase:              rs.Phase,
			RallyCount:         rs.RallyCount,
			MaxRallies:         p.MaxRallies,
			GoalMet:            rs.GoalMet,
			CanRerequest:       rs.CanRerequest,
			BlockReason:        rs.BlockReason,
			UnresolvedComments: unresolvedComments,
			NewComments:        newComments,
		})
	}

	return output.LoopView{
		Reviewers: reviewerViews,
		Done:      state.Done,
	}
}

// lastRallyTime returns the latest TriggerAction.At for the given reviewer identity,
// using case-insensitive name comparison. Returns the zero time if no trigger is found.
func lastRallyTime(identity reviewer.Identity, triggers []reviewer.TriggerAction) time.Time {
	var latest time.Time
	for _, t := range triggers {
		if t.Reviewer.Type != identity.Type {
			continue
		}
		if !strings.EqualFold(t.Reviewer.Name, identity.Name) {
			continue
		}
		if t.At.After(latest) {
			latest = t.At
		}
	}

	return latest
}

// identityKeyFromReviewerIdentity returns the canonical "type:name" key string
// matching [github.identityKey] without importing the github package.
func identityKeyFromReviewerIdentity(id reviewer.Identity) string {
	if id.Name == "" {
		return string(id.Type)
	}

	return string(id.Type) + ":" + id.Name
}
