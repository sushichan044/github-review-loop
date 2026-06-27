package github

import (
	"context"
	"strings"

	"github.com/sushichan044/mergeable-please/internal/core/reviewer"
)

// mapReviewState maps a GitHub GraphQL review state string to a [reviewer.ReviewState].
// Unknown states are mapped to [reviewer.ReviewStatePending].
func mapReviewState(graphqlState string) reviewer.ReviewState {
	switch graphqlState {
	case "APPROVED":
		return reviewer.ReviewStateApproved
	case "CHANGES_REQUESTED":
		return reviewer.ReviewStateChangesRequested
	case "COMMENTED":
		return reviewer.ReviewStateCommented
	case "DISMISSED":
		return reviewer.ReviewStateDismissed
	default:
		return reviewer.ReviewStatePending
	}
}

// FetchSnapshot fetches all required data from GitHub and assembles a
// [reviewer.Snapshot] for the given PR and policies.
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
	policies []reviewer.Policy,
) (reviewer.Snapshot, error) {
	timeline, err := fetchTimeline(ctx, client.gql, pr)
	if err != nil {
		return reviewer.Snapshot{}, err
	}

	threads, err := fetchReviewThreads(ctx, client.gql, pr)
	if err != nil {
		return reviewer.Snapshot{}, err
	}

	snapshot := reviewer.Snapshot{
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
func applyTimelineNode(snapshot *reviewer.Snapshot, node timelineNode, policies []reviewer.Policy) {
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
	snapshot *reviewer.Snapshot,
	e *reviewRequestedEvent,
	policies []reviewer.Policy,
) {
	// Prefer User login, then Bot login, then Mannequin login.
	// Team review requests have no single login identity and are skipped.
	login := e.RequestedReviewer.UserLogin
	if login == "" {
		login = e.RequestedReviewer.BotLogin
	}
	if login == "" {
		login = e.RequestedReviewer.MannequinLogin
	}
	if login == "" {
		// Team review request — not attributable to a single login identity.
		return
	}
	if identity, ok := ResolveIdentity(login, policies); ok {
		snapshot.Triggers = append(snapshot.Triggers, reviewer.TriggerAction{
			Reviewer: identity,
			At:       e.CreatedAt,
		})
	}
}

func applyPullRequestReview(
	snapshot *reviewer.Snapshot,
	r *pullRequestReviewEvent,
	policies []reviewer.Policy,
) {
	identity, ok := ResolveIdentity(r.AuthorLogin, policies)
	if !ok {
		return
	}
	snapshot.Reviews = append(snapshot.Reviews, reviewer.Review{
		Reviewer:           identity,
		State:              mapReviewState(r.State),
		CommitOID:          r.CommitOID,
		At:                 r.SubmittedAt,
		Body:               r.Body,
		ID:                 r.ID,
		InlineCommentCount: r.InlineCommentCount,
	})
}

// applyIssueComment checks whether the comment body matches a github-app policy
// trigger string. When it does, a TriggerAction is appended for that github-app
// reviewer, regardless of who posted the comment.
func applyIssueComment(
	snapshot *reviewer.Snapshot,
	c *issueCommentEvent,
	policies []reviewer.Policy,
) {
	for _, p := range policies {
		if p.Identity.Type != reviewer.ReviewerTypeGitHubApp {
			continue
		}
		if p.Trigger == "" {
			continue
		}
		if !strings.Contains(c.Body, p.Trigger) {
			continue
		}
		snapshot.Triggers = append(snapshot.Triggers, reviewer.TriggerAction{
			Reviewer: p.Identity,
			At:       c.CreatedAt,
		})
	}
}

func applyThread(snapshot *reviewer.Snapshot, t reviewThread, policies []reviewer.Policy) {
	if t.AuthorLogin == "" {
		return
	}
	identity, ok := ResolveIdentity(t.AuthorLogin, policies)
	if !ok {
		return
	}
	snapshot.Threads = append(snapshot.Threads, reviewer.Thread{
		Reviewer: identity,
		Resolved: t.IsResolved,
	})
}
