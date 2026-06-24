package github_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sushichan044/github-review-loop/internal/github"
	"github.com/sushichan044/github-review-loop/internal/output"
	"github.com/sushichan044/github-review-loop/internal/reviewloop"
)

func TestUnresolvedThreadComments_ReturnsOnlyUnresolved(t *testing.T) {
	t.Parallel()

	fq := newFakeQuerier()
	fq.on("PRTimeline", timelineFiller("head", nil, nil, nil))
	fq.on("PRReviewThreads", threadsFiller([]fakeThread{
		{
			AuthorLogin: "copilot",
			Body:        "Fix this",
			URL:         "https://github.com/o/r/pull/1#issuecomment-1",
			IsResolved:  false,
		},
		{
			AuthorLogin: "copilot",
			Body:        "Good job",
			URL:         "https://github.com/o/r/pull/1#issuecomment-2",
			IsResolved:  true,
		},
	}))

	policies := []reviewloop.Policy{
		{Identity: reviewloop.ReviewerIdentity{Type: reviewloop.ReviewerTypeGitHubCopilot}},
	}

	client := buildClient(fq)
	result, err := github.UnresolvedThreadComments(
		context.Background(),
		client,
		github.PR{Owner: "o", Repo: "r", Number: 1},
		policies,
	)
	require.NoError(t, err)

	comments, ok := result["github-copilot"]
	require.True(t, ok, "expected entry for github-copilot")
	require.Len(t, comments, 1)
	assert.Equal(t, "copilot", comments[0].Author)
	assert.Equal(t, "Fix this", comments[0].Body)
	assert.Equal(t, "https://github.com/o/r/pull/1#issuecomment-1", comments[0].URL)
}

func TestUnresolvedThreadComments_AttributedByIdentity(t *testing.T) {
	t.Parallel()

	fq := newFakeQuerier()
	fq.on("PRTimeline", timelineFiller("head", nil, nil, nil))
	fq.on("PRReviewThreads", threadsFiller([]fakeThread{
		{AuthorLogin: "alice", Body: "Please refactor", URL: "https://example.com/c1", IsResolved: false},
		{AuthorLogin: "unknown-bot", Body: "Should be ignored", URL: "https://example.com/c2", IsResolved: false},
	}))

	policies := []reviewloop.Policy{
		{Identity: reviewloop.ReviewerIdentity{Type: reviewloop.ReviewerTypeUser, Name: "alice"}},
	}

	client := buildClient(fq)
	result, err := github.UnresolvedThreadComments(
		context.Background(),
		client,
		github.PR{Owner: "o", Repo: "r", Number: 1},
		policies,
	)
	require.NoError(t, err)

	comments, ok := result["user:alice"]
	require.True(t, ok, "expected entry for user:alice")
	require.Len(t, comments, 1)
	assert.Equal(t, "alice", comments[0].Author)

	// unknown-bot should not appear
	assert.Len(t, result, 1)
}

func TestUnresolvedThreadComments_EmptyWhenAllResolved(t *testing.T) {
	t.Parallel()

	fq := newFakeQuerier()
	fq.on("PRTimeline", timelineFiller("head", nil, nil, nil))
	fq.on("PRReviewThreads", threadsFiller([]fakeThread{
		{AuthorLogin: "alice", Body: "LGTM", URL: "https://example.com/c1", IsResolved: true},
	}))

	policies := []reviewloop.Policy{
		{Identity: reviewloop.ReviewerIdentity{Type: reviewloop.ReviewerTypeUser, Name: "alice"}},
	}

	client := buildClient(fq)
	result, err := github.UnresolvedThreadComments(
		context.Background(),
		client,
		github.PR{Owner: "o", Repo: "r", Number: 1},
		policies,
	)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestUnresolvedThreadComments_MultipleReviewers(t *testing.T) {
	t.Parallel()

	fq := newFakeQuerier()
	fq.on("PRTimeline", timelineFiller("head", nil, nil, nil))
	fq.on("PRReviewThreads", threadsFiller([]fakeThread{
		{AuthorLogin: "alice", Body: "Alice comment", URL: "https://example.com/a1", IsResolved: false},
		{AuthorLogin: "copilot", Body: "Copilot comment", URL: "https://example.com/c1", IsResolved: false},
	}))

	policies := []reviewloop.Policy{
		{Identity: reviewloop.ReviewerIdentity{Type: reviewloop.ReviewerTypeUser, Name: "alice"}},
		{Identity: reviewloop.ReviewerIdentity{Type: reviewloop.ReviewerTypeGitHubCopilot}},
	}

	client := buildClient(fq)
	result, err := github.UnresolvedThreadComments(
		context.Background(),
		client,
		github.PR{Owner: "o", Repo: "r", Number: 1},
		policies,
	)
	require.NoError(t, err)
	assert.Len(t, result, 2)

	aliceComments := result["user:alice"]
	require.Len(t, aliceComments, 1)
	assert.Equal(t,
		output.CommentView{Author: "alice", Body: "Alice comment", URL: "https://example.com/a1"},
		aliceComments[0],
	)

	copilotComments := result["github-copilot"]
	require.Len(t, copilotComments, 1)
	assert.Equal(t,
		output.CommentView{Author: "copilot", Body: "Copilot comment", URL: "https://example.com/c1"},
		copilotComments[0],
	)
}
