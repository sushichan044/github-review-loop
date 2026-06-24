package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sushichan044/github-review-loop/internal/config"
	"github.com/sushichan044/github-review-loop/internal/reviewloop"
)

// --- Parse tests ---

func TestParse_OwnerScopeOnly(t *testing.T) {
	t.Parallel()

	yaml := `
loops:
  - scope: owner
    owner: acme
    reviewers:
      - type: user
        name: alice
        goal: { approved: true }
        max-rallies: 3
        trigger: "ping @alice"
`
	cfg, err := config.Parse([]byte(yaml))
	require.NoError(t, err)
	require.Len(t, cfg.Loops, 1)

	loop := cfg.Loops[0]
	assert.Equal(t, "owner", loop.Scope)
	assert.Equal(t, "acme", loop.Owner)
	assert.Empty(t, loop.Repo)
	require.Len(t, loop.Reviewers, 1)

	r := loop.Reviewers[0]
	assert.Equal(t, "user", r.Type)
	assert.Equal(t, "alice", r.Name)
	assert.True(t, r.Goal.Approved)
	assert.False(t, r.Goal.AllConversationsResolved)
	assert.Equal(t, 3, r.MaxRallies)
	assert.Equal(t, "ping @alice", r.Trigger)
}

func TestParse_RepoScope(t *testing.T) {
	t.Parallel()

	yaml := `
loops:
  - scope: repo
    owner: acme
    repo: myrepo
    reviewers:
      - type: github-copilot
        goal: { all-conversations-resolved: true }
`
	cfg, err := config.Parse([]byte(yaml))
	require.NoError(t, err)
	require.Len(t, cfg.Loops, 1)

	loop := cfg.Loops[0]
	assert.Equal(t, "repo", loop.Scope)
	assert.Equal(t, "acme", loop.Owner)
	assert.Equal(t, "myrepo", loop.Repo)

	r := loop.Reviewers[0]
	assert.Equal(t, "github-copilot", r.Type)
	assert.Empty(t, r.Name)
	assert.False(t, r.Goal.Approved)
	assert.True(t, r.Goal.AllConversationsResolved)
}

func TestParse_DefaultMaxRallies(t *testing.T) {
	t.Parallel()

	yaml := `
loops:
  - scope: owner
    owner: acme
    reviewers:
      - type: github-copilot
        goal: { all-conversations-resolved: true }
`
	cfg, err := config.Parse([]byte(yaml))
	require.NoError(t, err)
	assert.Equal(t, 5, cfg.Loops[0].Reviewers[0].MaxRallies)
}

func TestParse_GithubApp_WithTrigger(t *testing.T) {
	t.Parallel()

	yaml := `
loops:
  - scope: owner
    owner: acme
    reviewers:
      - type: github-app
        name: coderabbitai
        goal: { approved: true }
        trigger: "@coderabbitai review"
`
	cfg, err := config.Parse([]byte(yaml))
	require.NoError(t, err)

	r := cfg.Loops[0].Reviewers[0]
	assert.Equal(t, "github-app", r.Type)
	assert.Equal(t, "coderabbitai", r.Name)
	assert.Equal(t, "@coderabbitai review", r.Trigger)
	assert.Equal(t, 5, r.MaxRallies)
}

func TestParse_CopilotWithoutName_OK(t *testing.T) {
	t.Parallel()

	yaml := `
loops:
  - scope: owner
    owner: acme
    reviewers:
      - type: github-copilot
        goal: { all-conversations-resolved: true }
`
	cfg, err := config.Parse([]byte(yaml))
	require.NoError(t, err)
	assert.Empty(t, cfg.Loops[0].Reviewers[0].Name)
}

// --- Parse validation failure tests ---

func TestParse_Validation_GoalBothTrue(t *testing.T) {
	t.Parallel()

	yaml := `
loops:
  - scope: owner
    owner: acme
    reviewers:
      - type: user
        name: alice
        goal: { approved: true, all-conversations-resolved: true }
`
	_, err := config.Parse([]byte(yaml))
	assert.Error(t, err)
}

func TestParse_Validation_GoalNeitherTrue(t *testing.T) {
	t.Parallel()

	yaml := `
loops:
  - scope: owner
    owner: acme
    reviewers:
      - type: user
        name: alice
        goal: {}
`
	_, err := config.Parse([]byte(yaml))
	assert.Error(t, err)
}

func TestParse_Validation_BadType(t *testing.T) {
	t.Parallel()

	yaml := `
loops:
  - scope: owner
    owner: acme
    reviewers:
      - type: bot
        name: alice
        goal: { approved: true }
`
	_, err := config.Parse([]byte(yaml))
	assert.Error(t, err)
}

func TestParse_Validation_UserMissingName(t *testing.T) {
	t.Parallel()

	yaml := `
loops:
  - scope: owner
    owner: acme
    reviewers:
      - type: user
        goal: { approved: true }
`
	_, err := config.Parse([]byte(yaml))
	assert.Error(t, err)
}

func TestParse_Validation_GithubAppMissingName(t *testing.T) {
	t.Parallel()

	yaml := `
loops:
  - scope: owner
    owner: acme
    reviewers:
      - type: github-app
        goal: { approved: true }
`
	_, err := config.Parse([]byte(yaml))
	assert.Error(t, err)
}

func TestParse_Validation_BadScope(t *testing.T) {
	t.Parallel()

	yaml := `
loops:
  - scope: org
    owner: acme
    reviewers:
      - type: github-copilot
        goal: { all-conversations-resolved: true }
`
	_, err := config.Parse([]byte(yaml))
	assert.Error(t, err)
}

func TestParse_Validation_RepoScopeMissingRepo(t *testing.T) {
	t.Parallel()

	yaml := `
loops:
  - scope: repo
    owner: acme
    reviewers:
      - type: github-copilot
        goal: { all-conversations-resolved: true }
`
	_, err := config.Parse([]byte(yaml))
	assert.Error(t, err)
}

// --- Resolve tests ---

func makePolicy(rtype, name string, goal reviewloop.Goal, maxRallies int, trigger string) reviewloop.Policy {
	return reviewloop.Policy{
		Identity:   reviewloop.ReviewerIdentity{Type: reviewloop.ReviewerType(rtype), Name: name},
		Goal:       goal,
		MaxRallies: maxRallies,
		Trigger:    trigger,
	}
}

func TestResolve_OwnerOnlyReviewers(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Loops: []config.LoopEntry{
			{
				Scope: "owner",
				Owner: "acme",
				Reviewers: []config.ReviewerConfig{
					{Type: "user", Name: "alice", Goal: config.GoalConfig{Approved: true}, MaxRallies: 3},
				},
			},
		},
	}

	policies, err := config.Resolve(cfg, "acme", "myrepo")
	require.NoError(t, err)
	require.Len(t, policies, 1)
	assert.Equal(t, makePolicy("user", "alice", reviewloop.GoalApproved, 3, ""), policies[0])
}

func TestResolve_RepoOnlyReviewers(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Loops: []config.LoopEntry{
			{
				Scope: "repo",
				Owner: "acme",
				Repo:  "myrepo",
				Reviewers: []config.ReviewerConfig{
					{
						Type:       "github-copilot",
						Name:       "",
						Goal:       config.GoalConfig{AllConversationsResolved: true},
						MaxRallies: 5,
					},
				},
			},
		},
	}

	policies, err := config.Resolve(cfg, "acme", "myrepo")
	require.NoError(t, err)
	require.Len(t, policies, 1)
	assert.Equal(t, makePolicy("github-copilot", "", reviewloop.GoalAllConversationsResolved, 5, ""), policies[0])
}

func TestResolve_SameIdentityRepoOverridesOwner(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Loops: []config.LoopEntry{
			{
				Scope: "owner",
				Owner: "acme",
				Reviewers: []config.ReviewerConfig{
					{
						Type:       "github-copilot",
						Name:       "",
						Goal:       config.GoalConfig{AllConversationsResolved: true},
						MaxRallies: 5,
					},
				},
			},
			{
				Scope: "repo",
				Owner: "acme",
				Repo:  "myrepo",
				Reviewers: []config.ReviewerConfig{
					{
						Type:       "github-copilot",
						Name:       "",
						Goal:       config.GoalConfig{AllConversationsResolved: true},
						MaxRallies: 3,
					},
				},
			},
		},
	}

	policies, err := config.Resolve(cfg, "acme", "myrepo")
	require.NoError(t, err)
	require.Len(t, policies, 1)
	// repo overrides: max-rallies 3, not 5
	assert.Equal(t, 3, policies[0].MaxRallies)
}

func TestResolve_OwnerOnlyKept_RepoOnlyAdded(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Loops: []config.LoopEntry{
			{
				Scope: "owner",
				Owner: "acme",
				Reviewers: []config.ReviewerConfig{
					{Type: "user", Name: "alice", Goal: config.GoalConfig{Approved: true}, MaxRallies: 5},
				},
			},
			{
				Scope: "repo",
				Owner: "acme",
				Repo:  "myrepo",
				Reviewers: []config.ReviewerConfig{
					{
						Type:       "github-copilot",
						Name:       "",
						Goal:       config.GoalConfig{AllConversationsResolved: true},
						MaxRallies: 5,
					},
				},
			},
		},
	}

	policies, err := config.Resolve(cfg, "acme", "myrepo")
	require.NoError(t, err)
	require.Len(t, policies, 2)

	types := make(map[string]bool)
	for _, p := range policies {
		types[string(p.Identity.Type)] = true
	}
	assert.True(t, types["user"])
	assert.True(t, types["github-copilot"])
}

func TestResolve_CaseInsensitiveNameMatch(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Loops: []config.LoopEntry{
			{
				Scope: "owner",
				Owner: "acme",
				Reviewers: []config.ReviewerConfig{
					{Type: "user", Name: "Alice", Goal: config.GoalConfig{Approved: true}, MaxRallies: 5},
				},
			},
			{
				Scope: "repo",
				Owner: "acme",
				Repo:  "myrepo",
				Reviewers: []config.ReviewerConfig{
					{Type: "user", Name: "alice", Goal: config.GoalConfig{Approved: true}, MaxRallies: 2},
				},
			},
		},
	}

	policies, err := config.Resolve(cfg, "acme", "myrepo")
	require.NoError(t, err)
	// case-insensitive: "Alice" and "alice" are the same identity => repo wins
	require.Len(t, policies, 1)
	assert.Equal(t, 2, policies[0].MaxRallies)
}

func TestResolve_DifferentOwnerIgnored(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Loops: []config.LoopEntry{
			{
				Scope: "owner",
				Owner: "other-org",
				Reviewers: []config.ReviewerConfig{
					{Type: "user", Name: "alice", Goal: config.GoalConfig{Approved: true}, MaxRallies: 5},
				},
			},
		},
	}

	policies, err := config.Resolve(cfg, "acme", "myrepo")
	require.NoError(t, err)
	assert.Empty(t, policies)
}

func TestResolve_RepoMismatchIgnored(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Loops: []config.LoopEntry{
			{
				Scope: "repo",
				Owner: "acme",
				Repo:  "otherrepo",
				Reviewers: []config.ReviewerConfig{
					{
						Type:       "github-copilot",
						Name:       "",
						Goal:       config.GoalConfig{AllConversationsResolved: true},
						MaxRallies: 5,
					},
				},
			},
		},
	}

	policies, err := config.Resolve(cfg, "acme", "myrepo")
	require.NoError(t, err)
	assert.Empty(t, policies)
}

func TestResolve_GoalMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		goal     config.GoalConfig
		wantGoal reviewloop.Goal
	}{
		{
			name:     "approved",
			goal:     config.GoalConfig{Approved: true},
			wantGoal: reviewloop.GoalApproved,
		},
		{
			name:     "all-conversations-resolved",
			goal:     config.GoalConfig{AllConversationsResolved: true},
			wantGoal: reviewloop.GoalAllConversationsResolved,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := &config.Config{
				Loops: []config.LoopEntry{
					{
						Scope: "owner",
						Owner: "acme",
						Reviewers: []config.ReviewerConfig{
							{Type: "github-copilot", Name: "", Goal: tc.goal, MaxRallies: 5},
						},
					},
				},
			}
			policies, err := config.Resolve(cfg, "acme", "myrepo")
			require.NoError(t, err)
			require.Len(t, policies, 1)
			assert.Equal(t, tc.wantGoal, policies[0].Goal)
		})
	}
}

func TestResolve_TriggerCarried(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Loops: []config.LoopEntry{
			{
				Scope: "owner",
				Owner: "acme",
				Reviewers: []config.ReviewerConfig{
					{
						Type:       "github-app",
						Name:       "coderabbitai",
						Goal:       config.GoalConfig{Approved: true},
						MaxRallies: 5,
						Trigger:    "@coderabbitai review",
					},
				},
			},
		},
	}

	policies, err := config.Resolve(cfg, "acme", "myrepo")
	require.NoError(t, err)
	assert.Equal(t, "@coderabbitai review", policies[0].Trigger)
}

// --- Load tests (non-parallel: uses filesystem) ---

func TestLoad_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Point XDG_CONFIG_HOME to a directory that has no config file
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	_, err := config.Load()
	require.Error(t, err)
	assert.ErrorIs(t, err, os.ErrNotExist, "expected os.ErrNotExist, got: %v", err)
}

func TestLoad_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	cfgDir := filepath.Join(tmpDir, "github-review-loop")
	require.NoError(t, os.MkdirAll(cfgDir, 0o755))

	content := `
loops:
  - scope: owner
    owner: acme
    reviewers:
      - type: github-copilot
        goal: { all-conversations-resolved: true }
`
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(content), 0o600))

	cfg, err := config.Load()
	require.NoError(t, err)
	require.Len(t, cfg.Loops, 1)
	assert.Equal(t, "acme", cfg.Loops[0].Owner)
}
