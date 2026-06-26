package github

import (
	"context"
	"fmt"
	"time"

	graphql "github.com/cli/shurcooL-graphql"

	"github.com/sushichan044/mergeable-please/internal/core/reviewer"
)

// ThreadComment holds a single review thread comment attributed to a reviewer.
type ThreadComment struct {
	Author    string
	Body      string
	URL       string
	CreatedAt time.Time
	Resolved  bool
}

// reviewThread holds extracted data for one PR review thread.
type reviewThread struct {
	IsResolved  bool
	AuthorLogin string // first comment's author login
	Body        string // first comment's body
	URL         string // first comment's permalink URL
	CreatedAt   time.Time
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
							Body      string
							URL       string
							CreatedAt graphqlTime
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

// ThreadComments returns all thread comments (both resolved and unresolved) attributed
// per reviewer, keyed by the reviewer's identity string ("type:name", or "type" when name
// empty). Each comment carries Author, Body, URL, CreatedAt, and Resolved.
//
// Attribution uses [ResolveIdentity] — the same logic as [FetchSnapshot] — so only threads
// whose first-comment author resolves to a known policy identity are included.
func ThreadComments(
	ctx context.Context,
	client *Client,
	pr PR,
	policies []reviewer.Policy,
) (map[string][]ThreadComment, error) {
	threads, err := fetchReviewThreads(ctx, client.gql, pr)
	if err != nil {
		return nil, err
	}

	result := make(map[string][]ThreadComment)

	for _, t := range threads {
		if t.AuthorLogin == "" {
			continue
		}

		identity, ok := ResolveIdentity(t.AuthorLogin, policies)
		if !ok {
			continue
		}

		key := identityKey(identity)
		result[key] = append(result[key], ThreadComment{
			Author:    t.AuthorLogin,
			Body:      t.Body,
			URL:       t.URL,
			CreatedAt: t.CreatedAt,
			Resolved:  t.IsResolved,
		})
	}

	return result, nil
}

// identityKey returns the canonical "type:name" string for a reviewer identity.
// This matches the format used by output.formatIdentity.
func identityKey(id reviewer.Identity) string {
	if id.Name == "" {
		return string(id.Type)
	}

	return string(id.Type) + ":" + id.Name
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
				t.URL = first.URL
				t.CreatedAt = first.CreatedAt.Time
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
