package github_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sushichan044/github-review-loop/internal/github"
	"github.com/sushichan044/github-review-loop/internal/reviewloop"
)

func TestThreadComments_IncludesResolvedAndUnresolved(t *testing.T) {
	t.Parallel()

	at := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

	fq := newFakeQuerier()
	fq.on("PRTimeline", timelineFiller("head", nil, nil, nil))
	fq.on("PRReviewThreads", threadsFiller([]fakeThread{
		{AuthorLogin: "copilot", Body: "Fix this", URL: "https://example.com/c1", IsResolved: false, CreatedAt: at},
		{
			AuthorLogin: "copilot",
			Body:        "Good job",
			URL:         "https://example.com/c2",
			IsResolved:  true,
			CreatedAt:   at.Add(time.Hour),
		},
	}))

	policies := []reviewloop.Policy{
		{Identity: reviewloop.ReviewerIdentity{Type: reviewloop.ReviewerTypeGitHubCopilot}},
	}

	client := buildClient(fq)
	result, err := github.ThreadComments(
		context.Background(),
		client,
		github.PR{Owner: "o", Repo: "r", Number: 1},
		policies,
	)
	require.NoError(t, err)

	comments, ok := result["github-copilot"]
	require.True(t, ok, "expected entry for github-copilot")
	require.Len(t, comments, 2, "both resolved and unresolved should be returned")

	assert.Equal(t, "copilot", comments[0].Author)
	assert.Equal(t, "Fix this", comments[0].Body)
	assert.Equal(t, at, comments[0].CreatedAt)
	assert.False(t, comments[0].Resolved)

	assert.Equal(t, "copilot", comments[1].Author)
	assert.Equal(t, "Good job", comments[1].Body)
	assert.Equal(t, at.Add(time.Hour), comments[1].CreatedAt)
	assert.True(t, comments[1].Resolved)
}

func TestThreadComments_CreatedAtFlowsThrough(t *testing.T) {
	t.Parallel()

	at := time.Date(2024, 9, 15, 8, 30, 0, 0, time.UTC)

	fq := newFakeQuerier()
	fq.on("PRTimeline", timelineFiller("head", nil, nil, nil))
	fq.on("PRReviewThreads", threadsFiller([]fakeThread{
		{AuthorLogin: "alice", Body: "Please review", URL: "https://example.com/a1", IsResolved: false, CreatedAt: at},
	}))

	policies := []reviewloop.Policy{
		{Identity: reviewloop.ReviewerIdentity{Type: reviewloop.ReviewerTypeUser, Name: "alice"}},
	}

	client := buildClient(fq)
	result, err := github.ThreadComments(
		context.Background(),
		client,
		github.PR{Owner: "o", Repo: "r", Number: 1},
		policies,
	)
	require.NoError(t, err)

	comments, ok := result["user:alice"]
	require.True(t, ok, "expected entry for user:alice")
	require.Len(t, comments, 1)
	assert.Equal(t, at, comments[0].CreatedAt)
	assert.Equal(t, "alice", comments[0].Author)
	assert.False(t, comments[0].Resolved)
}

func TestThreadComments_UnresolvedFiltering(t *testing.T) {
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
	result, err := github.ThreadComments(
		context.Background(),
		client,
		github.PR{Owner: "o", Repo: "r", Number: 1},
		policies,
	)
	require.NoError(t, err)

	comments, ok := result["github-copilot"]
	require.True(t, ok, "expected entry for github-copilot")
	require.Len(t, comments, 2)

	// Caller can derive unresolved by filtering Resolved == false.
	var unresolved []github.ThreadComment
	for _, c := range comments {
		if !c.Resolved {
			unresolved = append(unresolved, c)
		}
	}
	require.Len(t, unresolved, 1)
	assert.Equal(t, "copilot", unresolved[0].Author)
	assert.Equal(t, "Fix this", unresolved[0].Body)
}

func TestThreadComments_AttributedByIdentity(t *testing.T) {
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
	result, err := github.ThreadComments(
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

func TestThreadComments_EmptyWhenAllResolved(t *testing.T) {
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
	result, err := github.ThreadComments(
		context.Background(),
		client,
		github.PR{Owner: "o", Repo: "r", Number: 1},
		policies,
	)
	require.NoError(t, err)

	// All comments are still returned; caller filters by Resolved.
	comments, ok := result["user:alice"]
	require.True(t, ok)
	require.Len(t, comments, 1)
	assert.True(t, comments[0].Resolved)
}

func TestThreadComments_MultipleReviewers(t *testing.T) {
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
	result, err := github.ThreadComments(
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
		github.ThreadComment{Author: "alice", Body: "Alice comment", URL: "https://example.com/a1"},
		aliceComments[0],
	)

	copilotComments := result["github-copilot"]
	require.Len(t, copilotComments, 1)
	assert.Equal(t,
		github.ThreadComment{Author: "copilot", Body: "Copilot comment", URL: "https://example.com/c1"},
		copilotComments[0],
	)
}

func TestThreadComments_UnknownAuthorDropped(t *testing.T) {
	t.Parallel()

	fq := newFakeQuerier()
	fq.on("PRTimeline", timelineFiller("head", nil, nil, nil))
	fq.on("PRReviewThreads", threadsFiller([]fakeThread{
		{AuthorLogin: "unknown-bot", Body: "Should be ignored", URL: "https://example.com/x1", IsResolved: false},
	}))

	policies := []reviewloop.Policy{
		{Identity: reviewloop.ReviewerIdentity{Type: reviewloop.ReviewerTypeUser, Name: "alice"}},
	}

	client := buildClient(fq)
	result, err := github.ThreadComments(
		context.Background(),
		client,
		github.PR{Owner: "o", Repo: "r", Number: 1},
		policies,
	)
	require.NoError(t, err)
	assert.Empty(t, result)
}
