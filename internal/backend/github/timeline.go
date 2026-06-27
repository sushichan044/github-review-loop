package github

import (
	"context"
	"fmt"
	"time"

	graphql "github.com/cli/shurcooL-graphql"
)

// timelineResult holds all data extracted from a single timeline query batch.
type timelineResult struct {
	HeadRefOID string
	Nodes      []timelineNode
}

// timelineNode is one item from the PR timeline.
type timelineNode struct {
	Typename string

	// ReviewRequestedEvent
	ReviewRequestedEvent *reviewRequestedEvent

	// PullRequestReview
	PullRequestReview *pullRequestReviewEvent

	// PullRequestCommit
	PullRequestCommit *pullRequestCommitEvent

	// HeadRefForcePushedEvent
	HeadRefForcePushedEvent *headRefForcePushedEvent

	// IssueComment
	IssueComment *issueCommentEvent
}

type reviewRequestedEvent struct {
	RequestedReviewer requestedReviewerUnion
	CreatedAt         time.Time
}

type requestedReviewerUnion struct {
	// User reviewer
	UserLogin string
	// Team reviewer
	TeamSlug string
	// Bot reviewer (e.g. github-copilot, github-app bots)
	BotLogin string
	// Mannequin reviewer
	MannequinLogin string
}

type pullRequestReviewEvent struct {
	AuthorLogin        string
	State              string
	CommitOID          string
	SubmittedAt        time.Time
	Body               string
	ID                 string
	InlineCommentCount int
}

type pullRequestCommitEvent struct {
	CommitOID     string
	CommittedDate time.Time
}

type headRefForcePushedEvent struct {
	AfterCommitOID string
}

type issueCommentEvent struct {
	AuthorLogin string
	Body        string
	CreatedAt   time.Time
}

// prTimelineNode is the raw GraphQL shape for a single PR timeline item.
// Extracted as a named type so it can be shared between [prTimelineQueryStruct]
// and [convertTimelineNode] without duplication.
//
// The union member pattern uses inline fragment tags ("... on TypeName").
// Fields not present in a given node are zero values.
type prTimelineNode struct {
	Typename string `graphql:"__typename"`

	ReviewRequestedEvent struct {
		RequestedReviewer struct {
			User struct {
				Login string
			} `graphql:"... on User"`
			Team struct {
				Slug string
			} `graphql:"... on Team"`
			Bot struct {
				Login string
			} `graphql:"... on Bot"`
			Mannequin struct {
				Login string
			} `graphql:"... on Mannequin"`
		}
		CreatedAt graphqlTime
	} `graphql:"... on ReviewRequestedEvent"`

	PullRequestReview struct {
		Author struct {
			Login string
		}
		State          string
		SubmittedAt    graphqlTime
		Body           string
		FullDatabaseID string `graphql:"fullDatabaseId"`
		Comments       struct {
			TotalCount int
		} `graphql:"comments"`
		Commit struct {
			Oid string
		}
	} `graphql:"... on PullRequestReview"`

	PullRequestCommit struct {
		Commit struct {
			Oid           string
			CommittedDate graphqlTime
		}
	} `graphql:"... on PullRequestCommit"`

	HeadRefForcePushedEvent struct {
		AfterCommit struct {
			Oid string
		}
	} `graphql:"... on HeadRefForcePushedEvent"`

	IssueComment struct {
		Author struct {
			Login string
		}
		Body      string
		CreatedAt graphqlTime
	} `graphql:"... on IssueComment"`
}

// prTimelineQueryStruct is the shurcooL-graphql struct used to query the
// PR timeline. We fetch only the event types needed by FetchSnapshot.
type prTimelineQueryStruct struct {
	Repository struct {
		PullRequest struct {
			HeadRefOid    string `graphql:"headRefOid"`
			TimelineItems struct {
				TotalCount int32
				Nodes      []prTimelineNode
			} `graphql:"timelineItems(first: 100, skip: $skip)"`
		} `graphql:"pullRequest(number: $number)"`
	} `graphql:"repository(owner: $owner, name: $repo)"`
}

// fetchTimeline retrieves all timeline nodes for the given PR via skip-based
// pagination. It also returns the head commit OID from the first page.
func fetchTimeline(ctx context.Context, gql GraphQLQuerier, pr PR) (timelineResult, error) {
	const pageSize = 100

	var result timelineResult

	for skip := 0; ; skip += pageSize {
		if err := ctx.Err(); err != nil {
			return timelineResult{}, err
		}

		var q prTimelineQueryStruct
		vars := map[string]any{
			gqlVarOwner:  graphql.String(pr.Owner),
			gqlVarRepo:   graphql.String(pr.Repo),
			gqlVarNumber: graphql.Int(int32(pr.Number)), //nolint:gosec // PR numbers won't overflow int32
			"skip":       graphql.Int(int32(skip)),
		}

		if err := gql.QueryWithContext(ctx, "PRTimeline", &q, vars); err != nil {
			return timelineResult{}, fmt.Errorf("timeline query failed (skip=%d): %w", skip, err)
		}

		if skip == 0 {
			result.HeadRefOID = q.Repository.PullRequest.HeadRefOid
		}

		items := q.Repository.PullRequest.TimelineItems
		for _, n := range items.Nodes {
			node := convertTimelineNode(n)
			result.Nodes = append(result.Nodes, node)
		}

		// Stop when we've fetched all items.
		// Note: skip-based pagination can skip or duplicate items if the PR's timeline
		// TotalCount changes between page fetches on a live PR; acceptable for the
		// review-loop's controlled context.
		if int32(skip)+pageSize >= items.TotalCount {
			break
		}
	}

	return result, nil
}

// convertTimelineNode maps the raw GraphQL struct into our typed timelineNode.
func convertTimelineNode(n prTimelineNode) timelineNode {
	node := timelineNode{Typename: n.Typename}
	switch n.Typename {
	case "ReviewRequestedEvent":
		node.ReviewRequestedEvent = &reviewRequestedEvent{
			RequestedReviewer: requestedReviewerUnion{
				UserLogin:      n.ReviewRequestedEvent.RequestedReviewer.User.Login,
				TeamSlug:       n.ReviewRequestedEvent.RequestedReviewer.Team.Slug,
				BotLogin:       n.ReviewRequestedEvent.RequestedReviewer.Bot.Login,
				MannequinLogin: n.ReviewRequestedEvent.RequestedReviewer.Mannequin.Login,
			},
			CreatedAt: n.ReviewRequestedEvent.CreatedAt.Time,
		}
	case "PullRequestReview":
		node.PullRequestReview = &pullRequestReviewEvent{
			AuthorLogin:        n.PullRequestReview.Author.Login,
			State:              n.PullRequestReview.State,
			CommitOID:          n.PullRequestReview.Commit.Oid,
			SubmittedAt:        n.PullRequestReview.SubmittedAt.Time,
			Body:               n.PullRequestReview.Body,
			ID:                 n.PullRequestReview.FullDatabaseID,
			InlineCommentCount: n.PullRequestReview.Comments.TotalCount,
		}
	case "PullRequestCommit":
		node.PullRequestCommit = &pullRequestCommitEvent{
			CommitOID:     n.PullRequestCommit.Commit.Oid,
			CommittedDate: n.PullRequestCommit.Commit.CommittedDate.Time,
		}
	case "HeadRefForcePushedEvent":
		node.HeadRefForcePushedEvent = &headRefForcePushedEvent{
			AfterCommitOID: n.HeadRefForcePushedEvent.AfterCommit.Oid,
		}
	case "IssueComment":
		node.IssueComment = &issueCommentEvent{
			AuthorLogin: n.IssueComment.Author.Login,
			Body:        n.IssueComment.Body,
			CreatedAt:   n.IssueComment.CreatedAt.Time,
		}
	}
	return node
}

// graphqlTime wraps [time.Time] to unmarshal GitHub's RFC3339 timestamps.
type graphqlTime struct {
	time.Time
}

func (t *graphqlTime) UnmarshalJSON(data []byte) error {
	// Accept JSON null — GitHub can return null for timestamps on draft events.
	if string(data) == "null" {
		t.Time = time.Time{}
		return nil
	}

	// Strip surrounding quotes.
	if len(data) < 2 || data[0] != '"' || data[len(data)-1] != '"' {
		return fmt.Errorf("graphqlTime: expected JSON string or null, got %s", data)
	}
	s := string(data[1 : len(data)-1])
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return fmt.Errorf("graphqlTime: parse %q: %w", s, err)
	}
	t.Time = parsed
	return nil
}
