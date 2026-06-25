package cmd

import (
	"strings"
	"time"

	"github.com/sushichan044/github-review-loop/internal/github"
	"github.com/sushichan044/github-review-loop/internal/output"
	"github.com/sushichan044/github-review-loop/internal/reviewloop"
)

// buildLoopView maps a [reviewloop.LoopState] and its corresponding policies into an
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
	state reviewloop.LoopState,
	snapshot reviewloop.Snapshot,
	policies []reviewloop.Policy,
	allCommentsByKey map[string][]github.ThreadComment,
) output.LoopView {
	policyByIdentity := make(map[reviewloop.ReviewerIdentity]reviewloop.Policy, len(policies))
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
func lastRallyTime(identity reviewloop.ReviewerIdentity, triggers []reviewloop.TriggerAction) time.Time {
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
func identityKeyFromReviewerIdentity(id reviewloop.ReviewerIdentity) string {
	if id.Name == "" {
		return string(id.Type)
	}

	return string(id.Type) + ":" + id.Name
}
