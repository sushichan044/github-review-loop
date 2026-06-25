package cmd_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sushichan044/github-review-loop/cmd"
	"github.com/sushichan044/github-review-loop/internal/config"
	"github.com/sushichan044/github-review-loop/internal/github"
	"github.com/sushichan044/github-review-loop/internal/reviewloop"
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

// minimalConfig returns a config with a user reviewer and a copilot reviewer.
func minimalConfig() *config.Config {
	raw := `
reviewers:
  - type: user
    name: alice
    goal:
      approved: true
    max-rallies: 3
  - type: github-copilot
    goal:
      all-conversations-resolved: true
    max-rallies: 5
`

	cfg, err := config.Parse([]byte(raw))
	if err != nil {
		panic("minimalConfig: " + err.Error())
	}

	return cfg
}

// ---------------------------------------------------------------------------
// status command tests
// ---------------------------------------------------------------------------

func TestStatus_HumanFormat(t *testing.T) {
	t.Parallel()

	at := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

	aliceIdentity := reviewloop.ReviewerIdentity{Type: reviewloop.ReviewerTypeUser, Name: "alice"}
	copilotIdentity := reviewloop.ReviewerIdentity{Type: reviewloop.ReviewerTypeGitHubCopilot}

	snapshot := reviewloop.Snapshot{
		HeadCommitOID: "commitABC",
		Triggers: []reviewloop.TriggerAction{
			{Reviewer: aliceIdentity, At: at.Add(-time.Hour)},
		},
		Reviews: []reviewloop.Review{
			{Reviewer: aliceIdentity, State: reviewloop.ReviewStateApproved, CommitOID: "commitABC", At: at},
		},
		Threads: []reviewloop.Thread{
			{Reviewer: copilotIdentity, Resolved: false},
		},
	}

	// threadComments returns an unresolved comment for copilot.
	threadComments := map[string][]github.ThreadComment{
		"github-copilot": {
			{
				Author:   "copilot",
				Body:     "Please fix the import",
				URL:      "https://github.com/o/r/pull/1#r1",
				Resolved: false,
			},
		},
	}

	resolver := &fakePRResolver{owner: "myorg", repo: "myrepo", number: 1}
	cfg := minimalConfig()

	var buf bytes.Buffer

	err := cmd.RunStatusForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			FetchSnapshot: func(_ context.Context, _ github.PR, _ []reviewloop.Policy) (reviewloop.Snapshot, error) {
				return snapshot, nil
			},
			ThreadComments: func(_ context.Context, _ github.PR, _ []reviewloop.Policy) (map[string][]github.ThreadComment, error) {
				return threadComments, nil
			},
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
	snapshot := reviewloop.Snapshot{
		HeadCommitOID: "commitXYZ",
	}

	resolver := &fakePRResolver{owner: "myorg", repo: "myrepo", number: 2}
	cfg := minimalConfig()

	var buf bytes.Buffer

	err := cmd.RunStatusForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			FetchSnapshot: func(_ context.Context, _ github.PR, _ []reviewloop.Policy) (reviewloop.Snapshot, error) {
				return snapshot, nil
			},
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

	snapshot := reviewloop.Snapshot{
		HeadCommitOID: "commitXYZ",
	}

	resolver := &fakePRResolver{owner: "myorg", repo: "myrepo", number: 3}
	cfg := minimalConfig()

	var buf bytes.Buffer

	err := cmd.RunStatusForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			FetchSnapshot: func(_ context.Context, _ github.PR, _ []reviewloop.Policy) (reviewloop.Snapshot, error) {
				return snapshot, nil
			},
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
// NewComments tests
// ---------------------------------------------------------------------------

func TestStatus_NewComments_OnlyAfterLastRally(t *testing.T) {
	t.Parallel()

	rallyAt := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	beforeRally := rallyAt.Add(-time.Hour)
	afterRally := rallyAt.Add(time.Hour)

	aliceIdentity := reviewloop.ReviewerIdentity{Type: reviewloop.ReviewerTypeUser, Name: "alice"}

	snapshot := reviewloop.Snapshot{
		HeadCommitOID: "commitABC",
		Triggers: []reviewloop.TriggerAction{
			{Reviewer: aliceIdentity, At: rallyAt},
		},
	}

	// threadComments: one before rally, one after rally, both unresolved.
	threadComments := map[string][]github.ThreadComment{
		"user:alice": {
			{Author: "alice", Body: "old comment", URL: "https://github.com/o/r/pull/1#r1", CreatedAt: beforeRally},
			{Author: "alice", Body: "new comment", URL: "https://github.com/o/r/pull/1#r2", CreatedAt: afterRally},
		},
	}

	resolver := &fakePRResolver{owner: "myorg", repo: "myrepo", number: 1}
	cfg := minimalConfig()

	var buf bytes.Buffer

	err := cmd.RunStatusForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			FetchSnapshot: func(_ context.Context, _ github.PR, _ []reviewloop.Policy) (reviewloop.Snapshot, error) {
				return snapshot, nil
			},
			ThreadComments: func(_ context.Context, _ github.PR, _ []reviewloop.Policy) (map[string][]github.ThreadComment, error) {
				return threadComments, nil
			},
			LoadConfig: func() (*config.Config, error) { return cfg, nil },
			Out:        &buf,
		},
		"human",
		nil,
	)
	require.NoError(t, err)

	out := buf.String()
	// The after-rally comment must appear in the new-comments section.
	assert.Contains(t, out, "New comments since last rally")
	idx := strings.Index(out, "New comments since last rally")
	require.GreaterOrEqual(t, idx, 0, "new-comments section must be present")
	newSection := out[idx:]
	assert.Contains(t, newSection, "new comment", "after-rally comment should appear in new-comments section")
	assert.NotContains(t, newSection, "old comment", "before-rally comment must not appear in new-comments section")
}

func TestStatus_NewComments_AllNewWhenNoRally(t *testing.T) {
	t.Parallel()

	// No triggers for alice → last-rally is zero time → all comments count as new.
	commentAt := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

	snapshot := reviewloop.Snapshot{
		HeadCommitOID: "commitABC",
	}

	threadComments := map[string][]github.ThreadComment{
		"user:alice": {
			{
				Author:    "alice",
				Body:      "first comment ever",
				URL:       "https://github.com/o/r/pull/1#r1",
				CreatedAt: commentAt,
				Resolved:  false,
			},
		},
	}

	resolver := &fakePRResolver{owner: "myorg", repo: "myrepo", number: 1}
	cfg := minimalConfig()

	var buf bytes.Buffer

	err := cmd.RunStatusForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			FetchSnapshot: func(_ context.Context, _ github.PR, _ []reviewloop.Policy) (reviewloop.Snapshot, error) {
				return snapshot, nil
			},
			ThreadComments: func(_ context.Context, _ github.PR, _ []reviewloop.Policy) (map[string][]github.ThreadComment, error) {
				return threadComments, nil
			},
			LoadConfig: func() (*config.Config, error) { return cfg, nil },
			Out:        &buf,
		},
		"human",
		nil,
	)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "first comment ever")
}

// ---------------------------------------------------------------------------
// request command tests
// ---------------------------------------------------------------------------

func TestRequest_FiresOnlyCanRerequest(t *testing.T) {
	t.Parallel()

	at := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

	aliceIdentity := reviewloop.ReviewerIdentity{Type: reviewloop.ReviewerTypeUser, Name: "alice"}
	copilotIdentity := reviewloop.ReviewerIdentity{Type: reviewloop.ReviewerTypeGitHubCopilot}

	// alice: reviewed at head → CanRerequest = false (no new commit since last review)
	// copilot: has unresolved thread (not goal-met) and no review yet → CanRerequest = true
	snapshot := reviewloop.Snapshot{
		HeadCommitOID: "headCommit",
		Triggers: []reviewloop.TriggerAction{
			{Reviewer: aliceIdentity, At: at.Add(-time.Hour)},
		},
		Reviews: []reviewloop.Review{
			{Reviewer: aliceIdentity, State: reviewloop.ReviewStateApproved, CommitOID: "headCommit", At: at},
		},
		Threads: []reviewloop.Thread{
			{Reviewer: copilotIdentity, Resolved: false},
		},
	}

	exec := &captureExec{}
	triggerer := github.NewTriggererWithExec(exec.exec)

	resolver := &fakePRResolver{owner: "myorg", repo: "myrepo", number: 4}
	cfg := minimalConfig()

	var buf bytes.Buffer

	err := cmd.RunRequestForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			FetchSnapshot: func(_ context.Context, _ github.PR, _ []reviewloop.Policy) (reviewloop.Snapshot, error) {
				return snapshot, nil
			},
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

	snapshot := reviewloop.Snapshot{
		HeadCommitOID: "headCommit",
	}

	exec := &captureExec{}
	triggerer := github.NewTriggererWithExec(exec.exec)

	resolver := &fakePRResolver{owner: "myorg", repo: "myrepo", number: 5}
	cfg := minimalConfig()

	var buf bytes.Buffer

	err := cmd.RunRequestForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			FetchSnapshot: func(_ context.Context, _ github.PR, _ []reviewloop.Policy) (reviewloop.Snapshot, error) {
				return snapshot, nil
			},
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

	aliceIdentity := reviewloop.ReviewerIdentity{Type: reviewloop.ReviewerTypeUser, Name: "alice"}

	// alice reviewed the current head → blocked by "no new commit since last review"
	snapshot := reviewloop.Snapshot{
		HeadCommitOID: "headCommit",
		Triggers: []reviewloop.TriggerAction{
			{Reviewer: aliceIdentity, At: at.Add(-time.Hour)},
		},
		Reviews: []reviewloop.Review{
			{Reviewer: aliceIdentity, State: reviewloop.ReviewStateChangesRequested, CommitOID: "headCommit", At: at},
		},
	}

	exec := &captureExec{}
	triggerer := github.NewTriggererWithExec(exec.exec)

	resolver := &fakePRResolver{owner: "myorg", repo: "myrepo", number: 6}
	cfg := minimalConfig()

	var buf bytes.Buffer

	err := cmd.RunRequestForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			FetchSnapshot: func(_ context.Context, _ github.PR, _ []reviewloop.Policy) (reviewloop.Snapshot, error) {
				return snapshot, nil
			},
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

	snapshot := reviewloop.Snapshot{HeadCommitOID: "head"}
	cfg := minimalConfig()

	var buf bytes.Buffer

	err := cmd.RunStatusForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			FetchSnapshot: func(_ context.Context, _ github.PR, _ []reviewloop.Policy) (reviewloop.Snapshot, error) {
				return snapshot, nil
			},
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

	snapshot := reviewloop.Snapshot{HeadCommitOID: "head"}
	cfg := minimalConfig()

	var buf bytes.Buffer

	err := cmd.RunStatusForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			FetchSnapshot: func(_ context.Context, _ github.PR, _ []reviewloop.Policy) (reviewloop.Snapshot, error) {
				return snapshot, nil
			},
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

	snapshot := reviewloop.Snapshot{HeadCommitOID: "head"}
	cfg := minimalConfig()

	var buf bytes.Buffer

	err := cmd.RunStatusForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			FetchSnapshot: func(_ context.Context, _ github.PR, _ []reviewloop.Policy) (reviewloop.Snapshot, error) {
				return snapshot, nil
			},
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

// emptyPoliciesConfig returns a config with no reviewers, so config.Policies
// returns zero policies.
func emptyPoliciesConfig() *config.Config {
	cfg, err := config.Parse([]byte("reviewers: []\n"))
	if err != nil {
		panic("emptyPoliciesConfig: " + err.Error())
	}

	return cfg
}

func TestStatus_EmptyPolicies_ReturnsError(t *testing.T) {
	t.Parallel()

	resolver := &fakePRResolver{owner: "myorg", repo: "myrepo", number: 10}
	cfg := emptyPoliciesConfig()

	var buf bytes.Buffer

	err := cmd.RunStatusForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			FetchSnapshot: func(_ context.Context, _ github.PR, _ []reviewloop.Policy) (reviewloop.Snapshot, error) {
				return reviewloop.Snapshot{}, nil
			},
			LoadConfig: func() (*config.Config, error) { return cfg, nil },
			Out:        &buf,
		},
		"human",
		nil,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no reviewers configured in .github/review-loop.yml")
}

func TestRequest_EmptyPolicies_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := &captureExec{}
	triggerer := github.NewTriggererWithExec(exec.exec)

	resolver := &fakePRResolver{owner: "myorg", repo: "myrepo", number: 11}
	cfg := emptyPoliciesConfig()

	var buf bytes.Buffer

	err := cmd.RunRequestForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			FetchSnapshot: func(_ context.Context, _ github.PR, _ []reviewloop.Policy) (reviewloop.Snapshot, error) {
				return reviewloop.Snapshot{}, nil
			},
			Triggerer:  triggerer,
			LoadConfig: func() (*config.Config, error) { return cfg, nil },
			Out:        &buf,
		},
		"",
		nil,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no reviewers configured in .github/review-loop.yml")
	assert.Empty(t, exec.calls, "no review requests should fire when config is empty")
}

func TestParseFormat_InvalidValue_ReturnsError(t *testing.T) {
	t.Parallel()

	resolver := &fakePRResolver{owner: "o", repo: "r", number: 1}

	snapshot := reviewloop.Snapshot{HeadCommitOID: "head"}
	cfg := minimalConfig()

	var buf bytes.Buffer

	err := cmd.RunStatusForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			FetchSnapshot: func(_ context.Context, _ github.PR, _ []reviewloop.Policy) (reviewloop.Snapshot, error) {
				return snapshot, nil
			},
			LoadConfig: func() (*config.Config, error) { return cfg, nil },
			Out:        &buf,
		},
		"notaformat",
		nil,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "notaformat")
}

// ---------------------------------------------------------------------------
// Missing-config behavior (no-op + init hint)
// ---------------------------------------------------------------------------

func TestStatus_NoConfig_PrintsHintAndSucceeds(t *testing.T) {
	t.Parallel()

	resolver := &fakePRResolver{owner: "myorg", repo: "myrepo", number: 12}

	var buf bytes.Buffer

	err := cmd.RunStatusForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			FetchSnapshot: func(_ context.Context, _ github.PR, _ []reviewloop.Policy) (reviewloop.Snapshot, error) {
				return reviewloop.Snapshot{}, nil
			},
			LoadConfig: func() (*config.Config, error) { return nil, config.ErrConfigNotFound },
			Out:        &buf,
		},
		"human",
		nil,
	)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "init")
}

func TestStatus_NoConfig_HintWinsOverPRResolutionFailure(t *testing.T) {
	t.Parallel()

	// Config is checked before the PR is resolved, so an unconfigured repo gets
	// the init hint even when PR resolution would fail (no token / no open PR).
	resolver := &fakePRResolver{err: errors.New("gh: no pull requests found")}

	var buf bytes.Buffer

	err := cmd.RunStatusForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			FetchSnapshot: func(_ context.Context, _ github.PR, _ []reviewloop.Policy) (reviewloop.Snapshot, error) {
				return reviewloop.Snapshot{}, nil
			},
			LoadConfig: func() (*config.Config, error) { return nil, config.ErrConfigNotFound },
			Out:        &buf,
		},
		"human",
		nil,
	)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "init")
}

func TestRequest_NoConfig_PrintsHintAndFiresNothing(t *testing.T) {
	t.Parallel()

	exec := &captureExec{}
	triggerer := github.NewTriggererWithExec(exec.exec)

	resolver := &fakePRResolver{owner: "myorg", repo: "myrepo", number: 13}

	var buf bytes.Buffer

	err := cmd.RunRequestForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			FetchSnapshot: func(_ context.Context, _ github.PR, _ []reviewloop.Policy) (reviewloop.Snapshot, error) {
				return reviewloop.Snapshot{}, nil
			},
			Triggerer:  triggerer,
			LoadConfig: func() (*config.Config, error) { return nil, config.ErrConfigNotFound },
			Out:        &buf,
		},
		"",
		nil,
	)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "init")
	assert.Empty(t, exec.calls, "no review requests should fire when config is absent")
}

// ---------------------------------------------------------------------------
// init command tests
// ---------------------------------------------------------------------------

func TestInit_PrintsCreatedPath(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	err := cmd.RunInitForTest(cmd.TestDeps{
		InitConfig: func() (string, error) { return "/repo/.github/review-loop.yml", nil },
		Out:        &buf,
	})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "/repo/.github/review-loop.yml")
}

func TestInit_PropagatesError(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	err := cmd.RunInitForTest(cmd.TestDeps{
		InitConfig: func() (string, error) { return "", config.ErrConfigExists },
		Out:        &buf,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, config.ErrConfigExists)
}
