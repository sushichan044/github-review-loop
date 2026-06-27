package github_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sushichan044/mergeable-please/internal/backend/github"
	"github.com/sushichan044/mergeable-please/internal/core/reviewer"
)

// ---------------------------------------------------------------------------
// Fake querier
// ---------------------------------------------------------------------------

// fakeQuerier implements github.GraphQLQuerier. On each call it invokes the
// registered handler for the given query name, which receives the query struct
// by pointer and fills it directly.
type fakeQuerier struct {
	handlers map[string]func(q any) error
}

func newFakeQuerier() *fakeQuerier {
	return &fakeQuerier{handlers: make(map[string]func(q any) error)}
}

func (f *fakeQuerier) on(name string, fn func(q any) error) {
	f.handlers[name] = fn
}

func (f *fakeQuerier) QueryWithContext(_ context.Context, name string, q any, _ map[string]any) error {
	fn, ok := f.handlers[name]
	if !ok {
		return fmt.Errorf("fakeQuerier: unexpected query %q", name)
	}
	return fn(q)
}

// buildClient creates a *github.Client backed by a fake querier.
func buildClient(q *fakeQuerier) *github.Client {
	return github.NewClientWithQuerier(q)
}

// emptyThreadsFiller fills a reviewThreadsQueryStruct with empty results.
func emptyThreadsFiller(q any) error {
	return threadsFiller(nil)(q)
}

// ---------------------------------------------------------------------------
// Type aliases for the internal query structs so tests can fill them without
// depending on unexported types. We cast via the exported concrete types that
// FetchSnapshot passes to Query.
// ---------------------------------------------------------------------------

// These types mirror the internal query struct shapes so tests can cast and fill.
// Because the package uses unexported types for the query structs, we leverage
// the fact that FetchSnapshot calls Query("PRTimeline", &prTimelineQueryStruct{}, ...)
// and Query("PRReviewThreads", &reviewThreadsQueryStruct{}, ...).
// We reconstruct the shapes here by casting through interface{}.

// timelineFiller returns a handler that fills a PRTimeline query with the given data.
func timelineFiller(
	headOID string,
	reviews []fakeReview,
	reqEvents []fakeReviewRequest,
	comments []fakeIssueComment,
) func(q any) error {
	return func(q any) error {
		github.InjectTimeline(q, headOID, reviews, reqEvents, comments)
		return nil
	}
}

// threadsFiller returns a handler that fills a PRReviewThreads query.
func threadsFiller(threads []fakeThread) func(q any) error {
	return func(q any) error {
		github.InjectThreads(q, threads)
		return nil
	}
}

// fakeReview is test data for a PullRequestReview node.
type fakeReview = github.ExportedFakeReview

// fakeReviewRequest is test data for a ReviewRequestedEvent node.
type fakeReviewRequest = github.ExportedFakeReviewRequest

// fakeIssueComment is test data for an IssueComment node.
type fakeIssueComment = github.ExportedFakeIssueComment

// fakeThread is test data for a review thread.
type fakeThread = github.ExportedFakeThread

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestFetchSnapshot_ReviewStateMapping(t *testing.T) {
	t.Parallel()

	submitted := time.Date(2024, 1, 10, 12, 0, 0, 0, time.UTC)

	fq := newFakeQuerier()
	fq.on("PRTimeline", timelineFiller("abc123", []fakeReview{
		{AuthorLogin: "alice", State: "APPROVED", CommitOid: "abc123", SubmittedAt: submitted},
		{AuthorLogin: "alice", State: "CHANGES_REQUESTED", CommitOid: "def456", SubmittedAt: submitted.Add(time.Hour)},
	}, nil, nil))
	fq.on("PRReviewThreads", emptyThreadsFiller)

	policies := []reviewer.Policy{
		{Identity: reviewer.Identity{Type: reviewer.ReviewerTypeUser, Name: "alice"}},
	}

	snapshot, err := github.FetchSnapshot(
		context.Background(),
		buildClient(fq),
		github.PR{Owner: "o", Repo: "r", Number: 1},
		policies,
	)
	require.NoError(t, err)

	assert.Equal(t, "abc123", snapshot.HeadCommitOID)
	require.Len(t, snapshot.Reviews, 2)
	assert.Equal(t, reviewer.ReviewStateApproved, snapshot.Reviews[0].State)
	assert.Equal(t, reviewer.ReviewStateChangesRequested, snapshot.Reviews[1].State)
}

func TestFetchSnapshot_AllReviewStates(t *testing.T) {
	t.Parallel()

	at := time.Date(2024, 1, 10, 12, 0, 0, 0, time.UTC)
	graphqlStates := []string{"APPROVED", "CHANGES_REQUESTED", "COMMENTED", "DISMISSED", "PENDING"}
	expectedStates := []reviewer.ReviewState{
		reviewer.ReviewStateApproved,
		reviewer.ReviewStateChangesRequested,
		reviewer.ReviewStateCommented,
		reviewer.ReviewStateDismissed,
		reviewer.ReviewStatePending,
	}

	reviews := make([]fakeReview, len(graphqlStates))
	for i, s := range graphqlStates {
		reviews[i] = fakeReview{
			AuthorLogin: "alice",
			State:       s,
			CommitOid:   "head",
			SubmittedAt: at.Add(time.Duration(i) * time.Minute),
		}
	}

	fq := newFakeQuerier()
	fq.on("PRTimeline", timelineFiller("head", reviews, nil, nil))
	fq.on("PRReviewThreads", emptyThreadsFiller)

	policies := []reviewer.Policy{
		{Identity: reviewer.Identity{Type: reviewer.ReviewerTypeUser, Name: "alice"}},
	}

	snapshot, err := github.FetchSnapshot(
		context.Background(),
		buildClient(fq),
		github.PR{Owner: "o", Repo: "r", Number: 1},
		policies,
	)
	require.NoError(t, err)
	require.Len(t, snapshot.Reviews, len(expectedStates))
	for i, want := range expectedStates {
		assert.Equal(t, want, snapshot.Reviews[i].State, "index %d", i)
	}
}

func TestFetchSnapshot_TriggerAttribution(t *testing.T) {
	t.Parallel()

	at := time.Date(2024, 2, 1, 9, 0, 0, 0, time.UTC)

	fq := newFakeQuerier()
	fq.on("PRTimeline", timelineFiller("headOID", nil, []fakeReviewRequest{
		{UserLogin: "alice", CreatedAt: at},
	}, nil))
	fq.on("PRReviewThreads", emptyThreadsFiller)

	policies := []reviewer.Policy{
		{Identity: reviewer.Identity{Type: reviewer.ReviewerTypeUser, Name: "alice"}},
	}

	snapshot, err := github.FetchSnapshot(
		context.Background(),
		buildClient(fq),
		github.PR{Owner: "o", Repo: "r", Number: 1},
		policies,
	)
	require.NoError(t, err)
	require.Len(t, snapshot.Triggers, 1)
	assert.Equal(t, reviewer.ReviewerTypeUser, snapshot.Triggers[0].Reviewer.Type)
	assert.Equal(t, "alice", snapshot.Triggers[0].Reviewer.Name)
	assert.Equal(t, at, snapshot.Triggers[0].At)
}

func TestFetchSnapshot_IssueCommentTrigger(t *testing.T) {
	t.Parallel()

	at := time.Date(2024, 3, 1, 8, 0, 0, 0, time.UTC)

	policies := []reviewer.Policy{
		{
			Identity: reviewer.Identity{Type: reviewer.ReviewerTypeGitHubApp, Name: "my-bot"},
			Trigger:  "/review",
		},
	}

	t.Run("human author posting trigger string records trigger for github-app policy", func(t *testing.T) {
		t.Parallel()

		// Real-world scenario: a human posts "@coderabbitai review" or "/review please".
		// The trigger should be attributed to the github-app policy, not the human author.
		fq := newFakeQuerier()
		fq.on("PRTimeline", timelineFiller("headOID", nil, nil, []fakeIssueComment{
			{AuthorLogin: "alice", Body: "/review please", CreatedAt: at},
		}))
		fq.on("PRReviewThreads", emptyThreadsFiller)

		snapshot, err := github.FetchSnapshot(
			context.Background(),
			buildClient(fq),
			github.PR{Owner: "o", Repo: "r", Number: 1},
			policies,
		)
		require.NoError(t, err)
		require.Len(t, snapshot.Triggers, 1)
		assert.Equal(t, reviewer.ReviewerTypeGitHubApp, snapshot.Triggers[0].Reviewer.Type)
		assert.Equal(t, "my-bot", snapshot.Triggers[0].Reviewer.Name)
		assert.Equal(t, at, snapshot.Triggers[0].At)
	})

	t.Run("comment body not containing trigger string produces no trigger", func(t *testing.T) {
		t.Parallel()

		fq := newFakeQuerier()
		fq.on("PRTimeline", timelineFiller("headOID", nil, nil, []fakeIssueComment{
			{AuthorLogin: "alice", Body: "looks good to me", CreatedAt: at},
		}))
		fq.on("PRReviewThreads", emptyThreadsFiller)

		snapshot, err := github.FetchSnapshot(
			context.Background(),
			buildClient(fq),
			github.PR{Owner: "o", Repo: "r", Number: 1},
			policies,
		)
		require.NoError(t, err)
		assert.Empty(t, snapshot.Triggers)
	})
}

func TestFetchSnapshot_ThreadResolution(t *testing.T) {
	t.Parallel()

	fq := newFakeQuerier()
	fq.on("PRTimeline", timelineFiller("headOID", nil, nil, nil))
	fq.on("PRReviewThreads", threadsFiller([]fakeThread{
		{AuthorLogin: "copilot", Body: "Please fix this", IsResolved: false},
		{AuthorLogin: "copilot", Body: "Good job", IsResolved: true},
	}))

	policies := []reviewer.Policy{
		{Identity: reviewer.Identity{Type: reviewer.ReviewerTypeGitHubCopilot}},
	}

	snapshot, err := github.FetchSnapshot(
		context.Background(),
		buildClient(fq),
		github.PR{Owner: "o", Repo: "r", Number: 1},
		policies,
	)
	require.NoError(t, err)
	require.Len(t, snapshot.Threads, 2)
	assert.False(t, snapshot.Threads[0].Resolved)
	assert.True(t, snapshot.Threads[1].Resolved)
	assert.Equal(t, reviewer.ReviewerTypeGitHubCopilot, snapshot.Threads[0].Reviewer.Type)
}

func TestFetchSnapshot_UnknownReviewerDropped(t *testing.T) {
	t.Parallel()

	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	fq := newFakeQuerier()
	fq.on("PRTimeline", timelineFiller("headOID", []fakeReview{
		{AuthorLogin: "unknown-user", State: "APPROVED", CommitOid: "headOID", SubmittedAt: at},
	}, nil, nil))
	fq.on("PRReviewThreads", emptyThreadsFiller)

	policies := []reviewer.Policy{
		{Identity: reviewer.Identity{Type: reviewer.ReviewerTypeUser, Name: "alice"}},
	}

	snapshot, err := github.FetchSnapshot(
		context.Background(),
		buildClient(fq),
		github.PR{Owner: "o", Repo: "r", Number: 1},
		policies,
	)
	require.NoError(t, err)
	assert.Empty(t, snapshot.Reviews)
}

func TestFetchSnapshot_BotTriggerAttribution_Copilot(t *testing.T) {
	t.Parallel()

	at := time.Date(2024, 4, 1, 9, 0, 0, 0, time.UTC)

	fq := newFakeQuerier()
	fq.on("PRTimeline", timelineFiller("headOID", nil, []fakeReviewRequest{
		{BotLogin: "copilot-pull-request-reviewer", CreatedAt: at},
	}, nil))
	fq.on("PRReviewThreads", emptyThreadsFiller)

	policies := []reviewer.Policy{
		{Identity: reviewer.Identity{Type: reviewer.ReviewerTypeGitHubCopilot}},
	}

	snapshot, err := github.FetchSnapshot(
		context.Background(),
		buildClient(fq),
		github.PR{Owner: "o", Repo: "r", Number: 1},
		policies,
	)
	require.NoError(t, err)
	require.Len(t, snapshot.Triggers, 1)
	assert.Equal(t, reviewer.ReviewerTypeGitHubCopilot, snapshot.Triggers[0].Reviewer.Type)
	assert.Equal(t, at, snapshot.Triggers[0].At)
}

func TestFetchSnapshot_BotTriggerAttribution_GitHubApp(t *testing.T) {
	t.Parallel()

	at := time.Date(2024, 4, 2, 10, 0, 0, 0, time.UTC)

	fq := newFakeQuerier()
	fq.on("PRTimeline", timelineFiller("headOID", nil, []fakeReviewRequest{
		{BotLogin: "my-bot[bot]", CreatedAt: at},
	}, nil))
	fq.on("PRReviewThreads", emptyThreadsFiller)

	policies := []reviewer.Policy{
		{
			Identity: reviewer.Identity{Type: reviewer.ReviewerTypeGitHubApp, Name: "my-bot"},
		},
	}

	snapshot, err := github.FetchSnapshot(
		context.Background(),
		buildClient(fq),
		github.PR{Owner: "o", Repo: "r", Number: 1},
		policies,
	)
	require.NoError(t, err)
	require.Len(t, snapshot.Triggers, 1)
	assert.Equal(t, reviewer.ReviewerTypeGitHubApp, snapshot.Triggers[0].Reviewer.Type)
	assert.Equal(t, "my-bot", snapshot.Triggers[0].Reviewer.Name)
	assert.Equal(t, at, snapshot.Triggers[0].At)
}

func TestFetchSnapshot_HeadCommitOID(t *testing.T) {
	t.Parallel()

	fq := newFakeQuerier()
	fq.on("PRTimeline", timelineFiller("deadbeef", nil, nil, nil))
	fq.on("PRReviewThreads", emptyThreadsFiller)

	snapshot, err := github.FetchSnapshot(
		context.Background(),
		buildClient(fq),
		github.PR{Owner: "o", Repo: "r", Number: 1},
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "deadbeef", snapshot.HeadCommitOID)
}
