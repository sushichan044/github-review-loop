package github

import (
	"context"
	"fmt"

	graphql "github.com/cli/shurcooL-graphql"
)

// reviewThread holds extracted data for one PR review thread.
type reviewThread struct {
	IsResolved  bool
	AuthorLogin string // first comment's author login
	Body        string // first comment's body
}

// reviewThreadsQueryStruct is the shurcooL-graphql query struct for PR review threads.
type reviewThreadsQueryStruct struct {
	Repository struct {
		PullRequest struct {
			ReviewThreads struct {
				Nodes []struct {
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
				PageInfo struct {
					HasNextPage bool
					EndCursor   string
				}
			} `graphql:"reviewThreads(first: 100, after: $cursor)"`
		} `graphql:"pullRequest(number: $number)"`
	} `graphql:"repository(owner: $owner, name: $repo)"`
}

// fetchReviewThreads retrieves all review threads for the given PR using
// cursor-based pagination.
func fetchReviewThreads(_ context.Context, gql GraphQLQuerier, pr PR) ([]reviewThread, error) {
	var threads []reviewThread
	var cursor *graphql.String

	for {
		var q reviewThreadsQueryStruct
		vars := map[string]any{
			"owner":  graphql.String(pr.Owner),
			"repo":   graphql.String(pr.Repo),
			"number": graphql.Int(int32(pr.Number)), //nolint:gosec // PR numbers won't overflow int32
			"cursor": cursor,
		}

		if err := gql.Query("PRReviewThreads", &q, vars); err != nil {
			return nil, fmt.Errorf("review threads query failed: %w", err)
		}

		for _, node := range q.Repository.PullRequest.ReviewThreads.Nodes {
			t := reviewThread{IsResolved: node.IsResolved}
			if len(node.Comments.Nodes) > 0 {
				first := node.Comments.Nodes[0]
				t.AuthorLogin = first.Author.Login
				t.Body = first.Body
			}
			threads = append(threads, t)
		}

		pageInfo := q.Repository.PullRequest.ReviewThreads.PageInfo
		if !pageInfo.HasNextPage {
			break
		}
		c := graphql.String(pageInfo.EndCursor)
		cursor = &c
	}

	return threads, nil
}
