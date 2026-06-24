package cmd_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sushichan044/github-review-loop/cmd"
	"github.com/sushichan044/github-review-loop/internal/config"
	"github.com/sushichan044/github-review-loop/internal/github"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// fakePRResolver implements github.PRResolver.
type fakePRResolver struct {
	owner  string
	repo   string
	number int
	err    error
}

func (f *fakePRResolver) CurrentPR(_ context.Context) (string, string, int, error) {
	return f.owner, f.repo, f.number, f.err
}

// fakeQuerier implements github.GraphQLQuerier.
type fakeQuerier struct {
	handlers map[string]func(q any) error
}

func newFakeQuerier() *fakeQuerier {
	return &fakeQuerier{handlers: make(map[string]func(q any) error)}
}

func (f *fakeQuerier) on(name string, fn func(q any) error) {
	f.handlers[name] = fn
}

func (f *fakeQuerier) Query(name string, q any, _ map[string]any) error {
	fn, ok := f.handlers[name]
	if !ok {
		return fmt.Errorf("fakeQuerier: unexpected query %q", name)
	}

	return fn(q)
}

// fakeExec implements the ghExecFunc signature used by Triggerer.
type captureExec struct {
	calls [][]string
	err   error
}

func (c *captureExec) exec(args ...string) (bytes.Buffer, bytes.Buffer, error) {
	c.calls = append(c.calls, args)
	return bytes.Buffer{}, bytes.Buffer{}, c.err
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// minimalConfig returns a config with one user reviewer for owner/repo.
func minimalConfig(owner, repo string) *config.Config {
	raw := fmt.Sprintf(`
loops:
  - scope: owner
    owner: %s
    reviewers:
      - type: user
        name: alice
        goal:
          approved: true
        max-rallies: 3
  - scope: repo
    owner: %s
    repo: %s
    reviewers:
      - type: github-copilot
        goal:
          all-conversations-resolved: true
        max-rallies: 5
`, owner, owner, repo)

	cfg, err := config.Parse([]byte(raw))
	if err != nil {
		panic("minimalConfig: " + err.Error())
	}

	return cfg
}

// timelineFiller injects timeline data into a PRTimeline query struct.
// IssueComment events are omitted here because no cmd test needs them.
func timelineFiller(
	headOID string,
	reviews []github.FakeReview,
	reqEvents []github.FakeReviewRequest,
) func(q any) error {
	return func(q any) error {
		github.InjectTimeline(q, headOID, reviews, reqEvents, nil)
		return nil
	}
}

// threadsFiller injects thread data into a PRReviewThreads query struct.
func threadsFiller(threads []github.FakeThread) func(q any) error {
	return func(q any) error {
		github.InjectThreads(q, threads)
		return nil
	}
}

func emptyThreadsFiller(q any) error {
	github.InjectThreads(q, nil)
	return nil
}

// ---------------------------------------------------------------------------
// status command tests
// ---------------------------------------------------------------------------

func TestStatus_HumanFormat(t *testing.T) {
	t.Parallel()

	at := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

	fq := newFakeQuerier()
	fq.on("PRTimeline", timelineFiller(
		"commitABC",
		[]github.FakeReview{
			{AuthorLogin: "alice", State: "APPROVED", CommitOid: "commitABC", SubmittedAt: at},
		},
		[]github.FakeReviewRequest{
			{UserLogin: "alice", CreatedAt: at.Add(-time.Hour)},
		},
	))
	fq.on("PRReviewThreads", threadsFiller([]github.FakeThread{
		{
			AuthorLogin: "copilot",
			Body:        "Please fix the import",
			URL:         "https://github.com/o/r/pull/1#r1",
			IsResolved:  false,
		},
	}))

	resolver := &fakePRResolver{owner: "myorg", repo: "myrepo", number: 1}
	cfg := minimalConfig("myorg", "myrepo")

	var buf bytes.Buffer

	err := cmd.RunStatusForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver:   resolver,
			Client:     github.NewClientWithQuerier(fq),
			LoadConfig: func() (*config.Config, error) { return cfg, nil },
			Out:        &buf,
		},
		"human",
		nil,
	)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "Reviewer: user:alice")
	assert.Contains(t, out, "Phase:    goal-met")
	assert.Contains(t, out, "Rally:    1/3")
	assert.Contains(t, out, "Goal:     approved (met: true)")
	assert.Contains(t, out, "Goal met")

	assert.Contains(t, out, "Reviewer: github-copilot")
	assert.Contains(t, out, "Unresolved comments")
	assert.Contains(t, out, "Please fix the import")
}

func TestStatus_AgentFormat_BackgroundShellHint(t *testing.T) {
	t.Parallel()

	// Copilot with no review yet and no head commit → CanRerequest = true (initial request).
	fq := newFakeQuerier()
	fq.on("PRTimeline", timelineFiller("commitXYZ", nil, nil))
	fq.on("PRReviewThreads", emptyThreadsFiller)

	resolver := &fakePRResolver{owner: "myorg", repo: "myrepo", number: 2}
	cfg := minimalConfig("myorg", "myrepo")

	var buf bytes.Buffer

	err := cmd.RunStatusForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver:   resolver,
			Client:     github.NewClientWithQuerier(fq),
			LoadConfig: func() (*config.Config, error) { return cfg, nil },
			Out:        &buf,
		},
		"agent",
		nil,
	)
	require.NoError(t, err)

	out := buf.String()
	// Agent format should include the background shell hint.
	assert.Contains(t, out, "sleep 60")
	assert.Contains(t, out, "background")
}

func TestStatus_AgentFormat_NoBackgroundShellHintInHuman(t *testing.T) {
	t.Parallel()

	fq := newFakeQuerier()
	fq.on("PRTimeline", timelineFiller("commitXYZ", nil, nil))
	fq.on("PRReviewThreads", emptyThreadsFiller)

	resolver := &fakePRResolver{owner: "myorg", repo: "myrepo", number: 3}
	cfg := minimalConfig("myorg", "myrepo")

	var buf bytes.Buffer

	err := cmd.RunStatusForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver:   resolver,
			Client:     github.NewClientWithQuerier(fq),
			LoadConfig: func() (*config.Config, error) { return cfg, nil },
			Out:        &buf,
		},
		"human",
		nil,
	)
	require.NoError(t, err)

	out := buf.String()
	// Human format should NOT include the background shell hint.
	assert.NotContains(t, out, "sleep 60")
}

// ---------------------------------------------------------------------------
// request command tests
// ---------------------------------------------------------------------------

func TestRequest_FiresOnlyCanRerequest(t *testing.T) {
	t.Parallel()

	at := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

	// alice: reviewed at head → CanRerequest = false (no new commit since last review)
	// copilot: has unresolved thread (not goal-met) and no review yet → CanRerequest = true
	fq := newFakeQuerier()
	fq.on("PRTimeline", timelineFiller(
		"headCommit",
		[]github.FakeReview{
			{AuthorLogin: "alice", State: "APPROVED", CommitOid: "headCommit", SubmittedAt: at},
		},
		[]github.FakeReviewRequest{
			{UserLogin: "alice", CreatedAt: at.Add(-time.Hour)},
		},
	))
	// Give copilot an unresolved thread so GoalMet=false (keeps it Active).
	fq.on("PRReviewThreads", threadsFiller([]github.FakeThread{
		{AuthorLogin: "copilot", Body: "Fix this", URL: "", IsResolved: false},
	}))

	exec := &captureExec{}
	triggerer := github.NewTriggererWithExec(exec.exec)

	resolver := &fakePRResolver{owner: "myorg", repo: "myrepo", number: 4}
	cfg := minimalConfig("myorg", "myrepo")

	var buf bytes.Buffer

	err := cmd.RunRequestForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver:   resolver,
			Client:     github.NewClientWithQuerier(fq),
			Triggerer:  triggerer,
			LoadConfig: func() (*config.Config, error) { return cfg, nil },
			Out:        &buf,
		},
		"",
		nil,
	)
	require.NoError(t, err)

	out := buf.String()

	// alice should be SKIP (goal-met → terminal → CanRerequest false)
	assert.Contains(t, out, "SKIP  user:alice")

	// copilot should be FIRED (active, no prior review → initial request allowed)
	assert.Contains(t, out, "FIRED github-copilot")

	// Exactly one exec call (for copilot).
	require.Len(t, exec.calls, 1)
	assert.Contains(t, strings.Join(exec.calls[0], " "), "@copilot")
}

func TestRequest_ReviewerFlag_TargetsExactlyOne(t *testing.T) {
	t.Parallel()

	fq := newFakeQuerier()
	fq.on("PRTimeline", timelineFiller("headCommit", nil, nil))
	fq.on("PRReviewThreads", emptyThreadsFiller)

	exec := &captureExec{}
	triggerer := github.NewTriggererWithExec(exec.exec)

	resolver := &fakePRResolver{owner: "myorg", repo: "myrepo", number: 5}
	cfg := minimalConfig("myorg", "myrepo")

	var buf bytes.Buffer

	err := cmd.RunRequestForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver:   resolver,
			Client:     github.NewClientWithQuerier(fq),
			Triggerer:  triggerer,
			LoadConfig: func() (*config.Config, error) { return cfg, nil },
			Out:        &buf,
		},
		"user:alice",
		nil,
	)
	require.NoError(t, err)

	out := buf.String()

	// Only alice should appear in output.
	assert.Contains(t, out, "FIRED user:alice")
	assert.NotContains(t, out, "github-copilot")

	// Exactly one exec call for alice.
	require.Len(t, exec.calls, 1)
	assert.Contains(t, strings.Join(exec.calls[0], " "), "alice")
}

func TestRequest_BlockedReviewer_PrintsNoOpReason(t *testing.T) {
	t.Parallel()

	at := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

	// alice reviewed the current head → blocked by "no new commit since last review"
	fq := newFakeQuerier()
	fq.on("PRTimeline", timelineFiller(
		"headCommit",
		[]github.FakeReview{
			{AuthorLogin: "alice", State: "CHANGES_REQUESTED", CommitOid: "headCommit", SubmittedAt: at},
		},
		[]github.FakeReviewRequest{
			{UserLogin: "alice", CreatedAt: at.Add(-time.Hour)},
		},
	))
	fq.on("PRReviewThreads", emptyThreadsFiller)

	exec := &captureExec{}
	triggerer := github.NewTriggererWithExec(exec.exec)

	resolver := &fakePRResolver{owner: "myorg", repo: "myrepo", number: 6}
	cfg := minimalConfig("myorg", "myrepo")

	var buf bytes.Buffer

	err := cmd.RunRequestForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver:   resolver,
			Client:     github.NewClientWithQuerier(fq),
			Triggerer:  triggerer,
			LoadConfig: func() (*config.Config, error) { return cfg, nil },
			Out:        &buf,
		},
		"user:alice",
		nil,
	)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "SKIP  user:alice")
	assert.Contains(t, out, "no new commit since last review")
	assert.Empty(t, exec.calls, "no exec calls expected when blocked")
}

// ---------------------------------------------------------------------------
// PR resolution tests
// ---------------------------------------------------------------------------

func TestResolvePR_BareNumber_UsesCurrentRepo(t *testing.T) {
	t.Parallel()

	resolver := &fakePRResolver{owner: "org", repo: "rep", number: 99}

	fq := newFakeQuerier()
	fq.on("PRTimeline", timelineFiller("head", nil, nil))
	fq.on("PRReviewThreads", emptyThreadsFiller)

	cfg := minimalConfig("org", "rep")

	var buf bytes.Buffer

	err := cmd.RunStatusForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver:   resolver,
			Client:     github.NewClientWithQuerier(fq),
			LoadConfig: func() (*config.Config, error) { return cfg, nil },
			Out:        &buf,
		},
		"human",
		[]string{"42"},
	)
	// Should resolve without error (number 42 is combined with resolver's owner/repo).
	require.NoError(t, err)
}

func TestResolvePR_URL_ParsedDirectly(t *testing.T) {
	t.Parallel()

	// The resolver should NOT be called when a full URL is given.
	resolver := &fakePRResolver{err: errors.New("should not be called")}

	fq := newFakeQuerier()
	fq.on("PRTimeline", timelineFiller("head", nil, nil))
	fq.on("PRReviewThreads", emptyThreadsFiller)

	cfg := minimalConfig("myorg", "myrepo")

	var buf bytes.Buffer

	err := cmd.RunStatusForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver:   resolver,
			Client:     github.NewClientWithQuerier(fq),
			LoadConfig: func() (*config.Config, error) { return cfg, nil },
			Out:        &buf,
		},
		"human",
		[]string{"https://github.com/myorg/myrepo/pull/7"},
	)
	require.NoError(t, err)
}

func TestResolvePR_NoArg_DelegatesToResolver(t *testing.T) {
	t.Parallel()

	resolver := &fakePRResolver{owner: "myorg", repo: "myrepo", number: 8}

	fq := newFakeQuerier()
	fq.on("PRTimeline", timelineFiller("head", nil, nil))
	fq.on("PRReviewThreads", emptyThreadsFiller)

	cfg := minimalConfig("myorg", "myrepo")

	var buf bytes.Buffer

	err := cmd.RunStatusForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver:   resolver,
			Client:     github.NewClientWithQuerier(fq),
			LoadConfig: func() (*config.Config, error) { return cfg, nil },
			Out:        &buf,
		},
		"human",
		nil,
	)
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// ParseFormat / --format flag validation
// ---------------------------------------------------------------------------

func TestParseFormat_InvalidValue_ReturnsError(t *testing.T) {
	t.Parallel()

	resolver := &fakePRResolver{owner: "o", repo: "r", number: 1}

	fq := newFakeQuerier()
	fq.on("PRTimeline", timelineFiller("head", nil, nil))
	fq.on("PRReviewThreads", emptyThreadsFiller)

	cfg := minimalConfig("o", "r")

	var buf bytes.Buffer

	err := cmd.RunStatusForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver:   resolver,
			Client:     github.NewClientWithQuerier(fq),
			LoadConfig: func() (*config.Config, error) { return cfg, nil },
			Out:        &buf,
		},
		"notaformat",
		nil,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "notaformat")
}
