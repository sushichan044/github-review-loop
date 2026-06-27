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

	"github.com/sushichan044/mergeable-please/cmd"
	"github.com/sushichan044/mergeable-please/internal/backend/github"
	"github.com/sushichan044/mergeable-please/internal/config"
	"github.com/sushichan044/mergeable-please/internal/core"
	"github.com/sushichan044/mergeable-please/internal/core/reviewer"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

type fakePRResolver struct {
	owner  string
	repo   string
	number int
	err    error
}

func (f *fakePRResolver) CurrentPR(_ context.Context) (string, string, int, error) {
	return f.owner, f.repo, f.number, f.err
}

func (f *fakePRResolver) CurrentRepo(_ context.Context) (string, string, error) {
	return f.owner, f.repo, f.err
}

type captureExec struct {
	calls [][]string
	err   error
}

func (c *captureExec) exec(args ...string) (bytes.Buffer, bytes.Buffer, error) {
	c.calls = append(c.calls, args)
	return bytes.Buffer{}, bytes.Buffer{}, c.err
}

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

// configWithReviewers returns a Config with the given YAML-parsed reviewer list.
func configWithReviewers(yamlContent string) *config.Config {
	cfg, err := config.Parse([]byte(yamlContent))
	if err != nil {
		panic("configWithReviewers: " + err.Error())
	}
	return cfg
}

func defaultConfig() *config.Config {
	return configWithReviewers("")
}

func minimalConfig() *config.Config {
	return configWithReviewers(`
github:
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
`)
}

func emptyReviewersConfig() *config.Config {
	return configWithReviewers(`
github:
  reviewers: []
`)
}

// ---------------------------------------------------------------------------
// check command tests
// ---------------------------------------------------------------------------

func TestCheck_Satisfied_ExitsZero(t *testing.T) {
	t.Parallel()

	resolver := &fakePRResolver{owner: "org", repo: "repo", number: 1}
	satisfiedResult := core.CheckResult{Satisfied: true}

	var buf bytes.Buffer

	err := cmd.RunCheckForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			BundledEvaluate: func(_ context.Context, _ github.PR) (core.CheckResult, error) {
				return satisfiedResult, nil
			},
			LoadConfig: func() (*config.Config, error) { return defaultConfig(), nil },
			Out:        &buf,
		},
		nil,
	)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "status: satisfied")
}

func TestCheck_Blocked_ReturnsErrBlocked(t *testing.T) {
	t.Parallel()

	resolver := &fakePRResolver{owner: "org", repo: "repo", number: 1}
	blockedResult := core.CheckResult{
		Blockers: []core.Condition{
			{Kind: core.ConditionConflict, Severity: core.SeverityBlocker, Title: "Merge conflicts"},
		},
	}

	var buf bytes.Buffer

	err := cmd.RunCheckForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			BundledEvaluate: func(_ context.Context, _ github.PR) (core.CheckResult, error) {
				return blockedResult, nil
			},
			LoadConfig: func() (*config.Config, error) { return defaultConfig(), nil },
			Out:        &buf,
		},
		nil,
	)
	require.ErrorIs(t, err, cmd.ErrBlocked)
	assert.Contains(t, buf.String(), "status: blocked")
}

func TestCheck_WithReviewerLoop_NotDone_ReturnsErrBlocked(t *testing.T) {
	t.Parallel()

	resolver := &fakePRResolver{owner: "org", repo: "repo", number: 1}
	// BundledEvaluate returns no blockers, but reviewer loop is not done.
	snapshot := reviewer.Snapshot{
		HeadCommitOID: "head123",
		// No reviews → copilot goal (all-conversations-resolved) NOT met.
		Threads: []reviewer.Thread{
			{Reviewer: reviewer.Identity{Type: reviewer.ReviewerTypeGitHubCopilot}, Resolved: false},
		},
	}

	var buf bytes.Buffer

	err := cmd.RunCheckForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			BundledEvaluate: func(_ context.Context, _ github.PR) (core.CheckResult, error) {
				return core.CheckResult{}, nil // no blockers from merge state
			},
			FetchSnapshot: func(_ context.Context, _ github.PR, _ []reviewer.Policy) (reviewer.Snapshot, error) {
				return snapshot, nil
			},
			LoadConfig: func() (*config.Config, error) { return minimalConfig(), nil },
			Out:        &buf,
		},
		nil,
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, cmd.ErrBlocked)
}

func TestCheck_WithReviewerLoop_Done_ExitsZero(t *testing.T) {
	t.Parallel()

	resolver := &fakePRResolver{owner: "org", repo: "repo", number: 1}
	aliceIdentity := reviewer.Identity{Type: reviewer.ReviewerTypeUser, Name: "alice"}
	copilotIdentity := reviewer.Identity{Type: reviewer.ReviewerTypeGitHubCopilot}

	// alice approved, copilot has no unresolved threads → loop done.
	snapshot := reviewer.Snapshot{
		HeadCommitOID: "head123",
		Reviews: []reviewer.Review{
			{Reviewer: aliceIdentity, State: reviewer.ReviewStateApproved, CommitOID: "head123"},
		},
		// No threads for copilot → all-conversations-resolved = true.
		Threads: []reviewer.Thread{
			{Reviewer: copilotIdentity, Resolved: true},
		},
	}

	var buf bytes.Buffer

	err := cmd.RunCheckForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			BundledEvaluate: func(_ context.Context, _ github.PR) (core.CheckResult, error) {
				return core.CheckResult{}, nil
			},
			FetchSnapshot: func(_ context.Context, _ github.PR, _ []reviewer.Policy) (reviewer.Snapshot, error) {
				return snapshot, nil
			},
			LoadConfig: func() (*config.Config, error) { return minimalConfig(), nil },
			Out:        &buf,
		},
		nil,
	)
	require.NoError(t, err)
}

func TestCheck_Advisories_AlwaysShown_EvenWhenSatisfied(t *testing.T) {
	t.Parallel()

	resolver := &fakePRResolver{owner: "org", repo: "repo", number: 1}
	resultWithAdvisory := core.CheckResult{
		Advisories: []core.Condition{
			{
				Kind:     core.ConditionApprovalRequired,
				Severity: core.SeverityAdvisory,
				Title:    "Human approval required",
			},
		},
	}
	// No blockers → will be satisfied after Finalize.

	var buf bytes.Buffer

	err := cmd.RunCheckForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			BundledEvaluate: func(_ context.Context, _ github.PR) (core.CheckResult, error) {
				return resultWithAdvisory, nil
			},
			LoadConfig: func() (*config.Config, error) { return defaultConfig(), nil },
			Out:        &buf,
		},
		nil,
	)
	require.NoError(t, err, "advisories alone should not block")
	out := buf.String()
	assert.Contains(t, out, "status: satisfied")
	assert.Contains(t, out, "approval-required", "advisory must appear even when satisfied")
}

// ---------------------------------------------------------------------------
// PR resolution tests
// ---------------------------------------------------------------------------

func TestCheck_BareNumber_UsesCurrentRepo(t *testing.T) {
	t.Parallel()

	// Resolver returns owner/repo for the repo context; number must come from the arg.
	resolver := &fakePRResolver{owner: "org", repo: "rep"}

	var capturedPR github.PR
	var buf bytes.Buffer

	err := cmd.RunCheckForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			BundledEvaluate: func(_ context.Context, pr github.PR) (core.CheckResult, error) {
				capturedPR = pr
				return core.CheckResult{Satisfied: true}, nil
			},
			LoadConfig: func() (*config.Config, error) { return defaultConfig(), nil },
			Out:        &buf,
		},
		[]string{"42"},
	)
	require.NoError(t, err)
	assert.Equal(t, "org", capturedPR.Owner, "owner must come from CurrentRepo")
	assert.Equal(t, "rep", capturedPR.Repo, "repo must come from CurrentRepo")
	assert.Equal(t, 42, capturedPR.Number, "number must come from the argument")
}

func TestCheck_URL_ParsedDirectly(t *testing.T) {
	t.Parallel()

	// Resolver must never be called when a full URL is provided.
	resolver := &fakePRResolver{err: errors.New("should not be called")}

	var capturedPR github.PR
	var buf bytes.Buffer

	err := cmd.RunCheckForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			BundledEvaluate: func(_ context.Context, pr github.PR) (core.CheckResult, error) {
				capturedPR = pr
				return core.CheckResult{Satisfied: true}, nil
			},
			LoadConfig: func() (*config.Config, error) { return defaultConfig(), nil },
			Out:        &buf,
		},
		[]string{"https://github.com/myorg/myrepo/pull/7"},
	)
	require.NoError(t, err)
	assert.Equal(t, "myorg", capturedPR.Owner, "owner must come from URL")
	assert.Equal(t, "myrepo", capturedPR.Repo, "repo must come from URL")
	assert.Equal(t, 7, capturedPR.Number, "number must come from URL")
}

// ---------------------------------------------------------------------------
// request command tests
// ---------------------------------------------------------------------------

func TestRequest_FiresOnlyCanRerequest(t *testing.T) {
	t.Parallel()

	at := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

	aliceIdentity := reviewer.Identity{Type: reviewer.ReviewerTypeUser, Name: "alice"}
	copilotIdentity := reviewer.Identity{Type: reviewer.ReviewerTypeGitHubCopilot}

	snapshot := reviewer.Snapshot{
		HeadCommitOID: "headCommit",
		Triggers: []reviewer.TriggerAction{
			{Reviewer: aliceIdentity, At: at.Add(-time.Hour)},
		},
		Reviews: []reviewer.Review{
			{Reviewer: aliceIdentity, State: reviewer.ReviewStateApproved, CommitOID: "headCommit", At: at},
		},
		Threads: []reviewer.Thread{
			{Reviewer: copilotIdentity, Resolved: false},
		},
	}

	exec := &captureExec{}
	triggerer := github.NewTriggererWithExec(exec.exec)

	resolver := &fakePRResolver{owner: "myorg", repo: "myrepo", number: 4}

	var buf bytes.Buffer

	err := cmd.RunRequestForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			FetchSnapshot: func(_ context.Context, _ github.PR, _ []reviewer.Policy) (reviewer.Snapshot, error) {
				return snapshot, nil
			},
			Triggerer:  triggerer,
			LoadConfig: func() (*config.Config, error) { return minimalConfig(), nil },
			Out:        &buf,
		},
		"",
		nil,
	)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "SKIP  user:alice")
	assert.Contains(t, out, "FIRED github-copilot")
	require.Len(t, exec.calls, 1)
	assert.Contains(t, strings.Join(exec.calls[0], " "), "@copilot")
}

func TestRequest_ReviewerFlag_TargetsExactlyOne(t *testing.T) {
	t.Parallel()

	snapshot := reviewer.Snapshot{HeadCommitOID: "headCommit"}

	exec := &captureExec{}
	triggerer := github.NewTriggererWithExec(exec.exec)
	resolver := &fakePRResolver{owner: "myorg", repo: "myrepo", number: 5}

	var buf bytes.Buffer

	err := cmd.RunRequestForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			FetchSnapshot: func(_ context.Context, _ github.PR, _ []reviewer.Policy) (reviewer.Snapshot, error) {
				return snapshot, nil
			},
			Triggerer:  triggerer,
			LoadConfig: func() (*config.Config, error) { return minimalConfig(), nil },
			Out:        &buf,
		},
		"user:alice",
		nil,
	)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "FIRED user:alice")
	assert.NotContains(t, out, "github-copilot")
	require.Len(t, exec.calls, 1)
	assert.Contains(t, strings.Join(exec.calls[0], " "), "alice")
}

func TestRequest_BlockedReviewer_PrintsNoOpReason(t *testing.T) {
	t.Parallel()

	at := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	aliceIdentity := reviewer.Identity{Type: reviewer.ReviewerTypeUser, Name: "alice"}

	snapshot := reviewer.Snapshot{
		HeadCommitOID: "headCommit",
		Triggers: []reviewer.TriggerAction{
			{Reviewer: aliceIdentity, At: at.Add(-time.Hour)},
		},
		Reviews: []reviewer.Review{
			{Reviewer: aliceIdentity, State: reviewer.ReviewStateChangesRequested, CommitOID: "headCommit", At: at},
		},
	}

	exec := &captureExec{}
	triggerer := github.NewTriggererWithExec(exec.exec)
	resolver := &fakePRResolver{owner: "myorg", repo: "myrepo", number: 6}

	var buf bytes.Buffer

	err := cmd.RunRequestForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			FetchSnapshot: func(_ context.Context, _ github.PR, _ []reviewer.Policy) (reviewer.Snapshot, error) {
				return snapshot, nil
			},
			Triggerer:  triggerer,
			LoadConfig: func() (*config.Config, error) { return minimalConfig(), nil },
			Out:        &buf,
		},
		"user:alice",
		nil,
	)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "SKIP  user:alice")
	assert.Contains(t, out, "no new commit since last review")
	assert.Empty(t, exec.calls)
}

func TestRequest_NoReviewers_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := &captureExec{}
	triggerer := github.NewTriggererWithExec(exec.exec)
	resolver := &fakePRResolver{owner: "myorg", repo: "myrepo", number: 11}

	var buf bytes.Buffer

	err := cmd.RunRequestForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver:   resolver,
			Triggerer:  triggerer,
			LoadConfig: func() (*config.Config, error) { return emptyReviewersConfig(), nil },
			Out:        &buf,
		},
		"",
		nil,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no reviewers configured")
	assert.Empty(t, exec.calls)
}

// ---------------------------------------------------------------------------
// init command tests
// ---------------------------------------------------------------------------

func TestInit_PrintsCreatedPath(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	err := cmd.RunInitForTest(cmd.TestDeps{
		InitConfig: func() (string, error) { return "/repo/.mergeable-please.yml", nil },
		Out:        &buf,
	})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "/repo/.mergeable-please.yml")
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

// ---------------------------------------------------------------------------
// view command tests
// ---------------------------------------------------------------------------

func TestView_ChecksDimension_NoStatusLine(t *testing.T) {
	t.Parallel()

	resolver := &fakePRResolver{owner: "org", repo: "repo", number: 1}
	blockedResult := core.CheckResult{
		Blockers: []core.Condition{
			{Kind: core.ConditionCheckFailing, Severity: core.SeverityBlocker, Title: "CI failing", Detail: "lint"},
			{Kind: core.ConditionConflict, Severity: core.SeverityBlocker, Title: "Conflicts"},
		},
	}

	var buf bytes.Buffer
	err := cmd.RunViewForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			BundledEvaluate: func(_ context.Context, _ github.PR) (core.CheckResult, error) {
				return blockedResult, nil
			},
			LoadConfig: func() (*config.Config, error) { return defaultConfig(), nil },
			Out:        &buf,
		},
		"checks",
		nil,
	)
	require.NoError(t, err)
	out := buf.String()
	assert.NotContains(t, out, "status:", "dimension view must never emit a global status line")
	assert.Contains(t, out, "check-failing", "check condition must appear")
	assert.NotContains(t, out, "conflict", "conflict condition must be filtered out for checks dimension")
}

func TestView_ConflictsDimension_NoStatusLine(t *testing.T) {
	t.Parallel()

	resolver := &fakePRResolver{owner: "org", repo: "repo", number: 1}
	blockedResult := core.CheckResult{
		Blockers: []core.Condition{
			{Kind: core.ConditionCheckFailing, Severity: core.SeverityBlocker, Title: "CI failing"},
			{Kind: core.ConditionConflict, Severity: core.SeverityBlocker, Title: "Merge conflicts"},
		},
	}

	var buf bytes.Buffer
	err := cmd.RunViewForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			BundledEvaluate: func(_ context.Context, _ github.PR) (core.CheckResult, error) {
				return blockedResult, nil
			},
			LoadConfig: func() (*config.Config, error) { return defaultConfig(), nil },
			Out:        &buf,
		},
		"conflicts",
		nil,
	)
	require.NoError(t, err)
	out := buf.String()
	assert.NotContains(t, out, "status:", "dimension view must never emit a global status line")
	assert.Contains(t, out, "conflict", "conflict condition must appear")
	assert.NotContains(t, out, "check-failing", "check condition must be filtered out for conflicts dimension")
}

// ---------------------------------------------------------------------------
// NewComments tests (via view reviewers path, but tested via request loop data)
// ---------------------------------------------------------------------------

func TestCheck_ReviewerLoop_NewComments_OnlyAfterLastRally(t *testing.T) {
	t.Parallel()

	rallyAt := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	aliceIdentity := reviewer.Identity{Type: reviewer.ReviewerTypeUser, Name: "alice"}

	snapshot := reviewer.Snapshot{
		HeadCommitOID: "commitABC",
		Triggers: []reviewer.TriggerAction{
			{Reviewer: aliceIdentity, At: rallyAt},
		},
		// alice has not approved → loop not done
		Reviews: []reviewer.Review{
			{Reviewer: aliceIdentity, State: reviewer.ReviewStateChangesRequested, CommitOID: "commitABC"},
		},
	}

	resolver := &fakePRResolver{owner: "myorg", repo: "myrepo", number: 1}

	var buf bytes.Buffer

	err := cmd.RunCheckForTest(
		context.Background(),
		cmd.TestDeps{
			Resolver: resolver,
			BundledEvaluate: func(_ context.Context, _ github.PR) (core.CheckResult, error) {
				return core.CheckResult{}, nil
			},
			FetchSnapshot: func(_ context.Context, _ github.PR, _ []reviewer.Policy) (reviewer.Snapshot, error) {
				return snapshot, nil
			},
			LoadConfig: func() (*config.Config, error) { return minimalConfig(), nil },
			Out:        &buf,
		},
		nil,
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, cmd.ErrBlocked)
}
