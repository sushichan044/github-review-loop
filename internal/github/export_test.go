package github

import "time"

// FakeReview is test data for a PullRequestReview timeline node.
type FakeReview struct {
	AuthorLogin string
	State       string
	CommitOid   string
	SubmittedAt time.Time
}

// FakeReviewRequest is test data for a ReviewRequestedEvent timeline node.
type FakeReviewRequest struct {
	UserLogin string
	CreatedAt time.Time
}

// FakeIssueComment is test data for an IssueComment timeline node.
type FakeIssueComment struct {
	AuthorLogin string
	Body        string
	CreatedAt   time.Time
}

// FakeThread is test data for a review thread.
type FakeThread struct {
	AuthorLogin string
	Body        string
	IsResolved  bool
}

// InjectTimeline directly sets fields on a *prTimelineQueryStruct so that
// fakeQuerier tests can populate the query struct without JSON marshaling.
func InjectTimeline(
	q any,
	headOID string,
	reviews []FakeReview,
	reqEvents []FakeReviewRequest,
	comments []FakeIssueComment,
) {
	query, ok := q.(*prTimelineQueryStruct)
	if !ok {
		return
	}
	query.Repository.PullRequest.HeadRefOid = headOID

	totalCount := len(reviews) + len(reqEvents) + len(comments)
	query.Repository.PullRequest.TimelineItems.TotalCount = int32(totalCount)

	// Each fake item produces one node with the appropriate typename set and
	// all other inline-fragment fields left at zero values.
	type nodeType = struct {
		Typename string `graphql:"__typename"`

		ReviewRequestedEvent struct {
			RequestedReviewer struct {
				User struct {
					Login string
				} `graphql:"... on User"`
				Team struct {
					Slug string
				} `graphql:"... on Team"`
			}
			CreatedAt graphqlTime
		} `graphql:"... on ReviewRequestedEvent"`

		PullRequestReview struct {
			Author struct {
				Login string
			}
			State       string
			SubmittedAt graphqlTime
			Commit      struct {
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

	var nodes []nodeType

	for _, r := range reviews {
		var n nodeType
		n.Typename = "PullRequestReview"
		n.PullRequestReview.Author.Login = r.AuthorLogin
		n.PullRequestReview.State = r.State
		n.PullRequestReview.Commit.Oid = r.CommitOid
		n.PullRequestReview.SubmittedAt = graphqlTime{r.SubmittedAt}
		nodes = append(nodes, n)
	}

	for _, rr := range reqEvents {
		var n nodeType
		n.Typename = "ReviewRequestedEvent"
		n.ReviewRequestedEvent.RequestedReviewer.User.Login = rr.UserLogin
		n.ReviewRequestedEvent.CreatedAt = graphqlTime{rr.CreatedAt}
		nodes = append(nodes, n)
	}

	for _, c := range comments {
		var n nodeType
		n.Typename = "IssueComment"
		n.IssueComment.Author.Login = c.AuthorLogin
		n.IssueComment.Body = c.Body
		n.IssueComment.CreatedAt = graphqlTime{c.CreatedAt}
		nodes = append(nodes, n)
	}

	query.Repository.PullRequest.TimelineItems.Nodes = nodes
}

// InjectThreads directly sets fields on a *reviewThreadsQueryStruct.
func InjectThreads(q any, threads []FakeThread) {
	query, ok := q.(*reviewThreadsQueryStruct)
	if !ok {
		return
	}

	type nodeType = struct {
		IsResolved bool `graphql:"isResolved"`
		Comments   struct {
			Nodes []struct {
				Author struct {
					Login string
				}
				Body string
			}
		} `graphql:"comments(first: 1)"`
	}

	nodes := make([]nodeType, 0, len(threads))
	for _, t := range threads {
		var n nodeType
		n.IsResolved = t.IsResolved
		if t.AuthorLogin != "" {
			n.Comments.Nodes = []struct {
				Author struct {
					Login string
				}
				Body string
			}{
				{Body: t.Body},
			}
			n.Comments.Nodes[0].Author.Login = t.AuthorLogin
		}
		nodes = append(nodes, n)
	}
	query.Repository.PullRequest.ReviewThreads.Nodes = nodes
	query.Repository.PullRequest.ReviewThreads.PageInfo.HasNextPage = false
}
