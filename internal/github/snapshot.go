package github

import (
	"context"
	"strings"

	"github.com/sushichan044/github-review-loop/internal/reviewloop"
)

// mapReviewState maps a GitHub GraphQL review state string to a [reviewloop.ReviewState].
// Unknown states are mapped to [reviewloop.ReviewStatePending].
func mapReviewState(graphqlState string) reviewloop.ReviewState {
	switch graphqlState {
	case "APPROVED":
		return reviewloop.ReviewStateApproved
	case "CHANGES_REQUESTED":
		return reviewloop.ReviewStateChangesRequested
	case "COMMENTED":
		return reviewloop.ReviewStateCommented
	case "DISMISSED":
		return reviewloop.ReviewStateDismissed
	default:
		return reviewloop.ReviewStatePending
	}
}

// FetchSnapshot fetches all required data from GitHub and assembles a
// [reviewloop.Snapshot] for the given PR and policies.
//
// Assembly:
//   - HeadCommitOID: from PR headRefOid
//   - Reviews: each PullRequestReview whose author resolves to a known identity
//   - Threads: each review thread whose first-comment author resolves
//   - Triggers: ReviewRequestedEvents that target a known reviewer, plus
//     IssueComments whose body matches a github-app policy's Trigger string
func FetchSnapshot(
	ctx context.Context,
	client *Client,
	pr PR,
	policies []reviewloop.Policy,
) (reviewloop.Snapshot, error) {
	timeline, err := fetchTimeline(ctx, client.gql, pr)
	if err != nil {
		return reviewloop.Snapshot{}, err
	}

	threads, err := fetchReviewThreads(ctx, client.gql, pr)
	if err != nil {
		return reviewloop.Snapshot{}, err
	}

	snapshot := reviewloop.Snapshot{
		HeadCommitOID: timeline.HeadRefOID,
	}

	for _, node := range timeline.Nodes {
		applyTimelineNode(&snapshot, node, policies)
	}

	for _, t := range threads {
		applyThread(&snapshot, t, policies)
	}

	return snapshot, nil
}

// applyTimelineNode interprets one timeline node and appends to snapshot as needed.
func applyTimelineNode(snapshot *reviewloop.Snapshot, node timelineNode, policies []reviewloop.Policy) {
	switch {
	case node.ReviewRequestedEvent != nil:
		applyReviewRequestedEvent(snapshot, node.ReviewRequestedEvent, policies)
	case node.PullRequestReview != nil:
		applyPullRequestReview(snapshot, node.PullRequestReview, policies)
	case node.IssueComment != nil:
		applyIssueComment(snapshot, node.IssueComment, policies)
	}
}

func applyReviewRequestedEvent(
	snapshot *reviewloop.Snapshot,
	e *reviewRequestedEvent,
	policies []reviewloop.Policy,
) {
	login := e.RequestedReviewer.UserLogin
	if login == "" {
		// Team review request — not attributable to a single login identity.
		return
	}
	if identity, ok := ResolveIdentity(login, policies); ok {
		snapshot.Triggers = append(snapshot.Triggers, reviewloop.TriggerAction{
			Reviewer: identity,
			At:       e.CreatedAt,
		})
	}
}

func applyPullRequestReview(
	snapshot *reviewloop.Snapshot,
	r *pullRequestReviewEvent,
	policies []reviewloop.Policy,
) {
	identity, ok := ResolveIdentity(r.AuthorLogin, policies)
	if !ok {
		return
	}
	snapshot.Reviews = append(snapshot.Reviews, reviewloop.Review{
		Reviewer:  identity,
		State:     mapReviewState(r.State),
		CommitOID: r.CommitOID,
		At:        r.SubmittedAt,
	})
}

// applyIssueComment checks whether the comment body matches a github-app policy
// trigger string. When it does, and the comment author resolves to a known identity,
// a TriggerAction is appended.
func applyIssueComment(
	snapshot *reviewloop.Snapshot,
	c *issueCommentEvent,
	policies []reviewloop.Policy,
) {
	for _, p := range policies {
		if p.Identity.Type != reviewloop.ReviewerTypeGitHubApp {
			continue
		}
		if p.Trigger == "" {
			continue
		}
		if !strings.Contains(c.Body, p.Trigger) {
			continue
		}
		if identity, ok := ResolveIdentity(c.AuthorLogin, policies); ok {
			snapshot.Triggers = append(snapshot.Triggers, reviewloop.TriggerAction{
				Reviewer: identity,
				At:       c.CreatedAt,
			})
		}
	}
}

func applyThread(snapshot *reviewloop.Snapshot, t reviewThread, policies []reviewloop.Policy) {
	if t.AuthorLogin == "" {
		return
	}
	identity, ok := ResolveIdentity(t.AuthorLogin, policies)
	if !ok {
		return
	}
	snapshot.Threads = append(snapshot.Threads, reviewloop.Thread{
		Reviewer: identity,
		Resolved: t.IsResolved,
	})
}
