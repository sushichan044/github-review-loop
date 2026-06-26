package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sushichan044/mergeable-please/internal/config"
	"github.com/sushichan044/mergeable-please/internal/core/reviewer"
)

// --- Parse tests ---

func TestParse_UserReviewer(t *testing.T) {
	t.Parallel()

	yaml := `
reviewers:
  - type: user
    name: alice
    goal: { approved: true }
    max-rallies: 3
    trigger: "ping @alice"
`
	cfg, err := config.Parse([]byte(yaml))
	require.NoError(t, err)
	require.Len(t, cfg.Reviewers, 1)

	r := cfg.Reviewers[0]
	assert.Equal(t, "user", r.Type)
	assert.Equal(t, "alice", r.Name)
	assert.True(t, r.Goal.Approved)
	assert.False(t, r.Goal.AllConversationsResolved)
	assert.Equal(t, 3, r.MaxRallies)
	assert.Equal(t, "ping @alice", r.Trigger)
}

func TestParse_CopilotReviewer(t *testing.T) {
	t.Parallel()

	yaml := `
reviewers:
  - type: github-copilot
    goal: { all-conversations-resolved: true }
`
	cfg, err := config.Parse([]byte(yaml))
	require.NoError(t, err)
	require.Len(t, cfg.Reviewers, 1)

	r := cfg.Reviewers[0]
	assert.Equal(t, "github-copilot", r.Type)
	assert.Empty(t, r.Name)
	assert.False(t, r.Goal.Approved)
	assert.True(t, r.Goal.AllConversationsResolved)
}

func TestParse_DefaultMaxRallies(t *testing.T) {
	t.Parallel()

	yaml := `
reviewers:
  - type: github-copilot
    goal: { all-conversations-resolved: true }
`
	cfg, err := config.Parse([]byte(yaml))
	require.NoError(t, err)
	assert.Equal(t, 5, cfg.Reviewers[0].MaxRallies)
}

func TestParse_GithubApp_WithTrigger(t *testing.T) {
	t.Parallel()

	yaml := `
reviewers:
  - type: github-app
    name: coderabbitai
    goal: { approved: true }
    trigger: "@coderabbitai review"
`
	cfg, err := config.Parse([]byte(yaml))
	require.NoError(t, err)

	r := cfg.Reviewers[0]
	assert.Equal(t, "github-app", r.Type)
	assert.Equal(t, "coderabbitai", r.Name)
	assert.Equal(t, "@coderabbitai review", r.Trigger)
	assert.Equal(t, 5, r.MaxRallies)
}

// --- Parse validation failure tests ---

func TestParse_Validation_GoalBothTrue(t *testing.T) {
	t.Parallel()

	yaml := `
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
reviewers:
  - type: github-app
    goal: { approved: true }
`
	_, err := config.Parse([]byte(yaml))
	assert.Error(t, err)
}

// --- Policies tests ---

func makePolicy(rtype, name string, goal reviewer.Goal, maxRallies int, trigger string) reviewer.Policy {
	return reviewer.Policy{
		Identity:   reviewer.Identity{Type: reviewer.Type(rtype), Name: name},
		Goal:       goal,
		MaxRallies: maxRallies,
		Trigger:    trigger,
	}
}

func TestPolicies_MapsEachReviewer(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Reviewers: []config.ReviewerConfig{
			{Type: "user", Name: "alice", Goal: config.GoalConfig{Approved: true}, MaxRallies: 3},
			{
				Type:       "github-copilot",
				Goal:       config.GoalConfig{AllConversationsResolved: true},
				MaxRallies: 5,
			},
		},
	}

	policies, err := config.Policies(cfg)
	require.NoError(t, err)
	require.Len(t, policies, 2)
	assert.Equal(t, makePolicy("user", "alice", reviewer.GoalApproved, 3, ""), policies[0])
	assert.Equal(
		t,
		makePolicy("github-copilot", "", reviewer.GoalAllConversationsResolved, 5, ""),
		policies[1],
	)
}

func TestPolicies_Empty(t *testing.T) {
	t.Parallel()

	policies, err := config.Policies(&config.Config{})
	require.NoError(t, err)
	assert.Empty(t, policies)
}

func TestPolicies_GoalMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		goal     config.GoalConfig
		wantGoal reviewer.Goal
	}{
		{
			name:     "approved",
			goal:     config.GoalConfig{Approved: true},
			wantGoal: reviewer.GoalApproved,
		},
		{
			name:     "all-conversations-resolved",
			goal:     config.GoalConfig{AllConversationsResolved: true},
			wantGoal: reviewer.GoalAllConversationsResolved,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := &config.Config{
				Reviewers: []config.ReviewerConfig{
					{Type: "github-copilot", Goal: tc.goal, MaxRallies: 5},
				},
			}
			policies, err := config.Policies(cfg)
			require.NoError(t, err)
			require.Len(t, policies, 1)
			assert.Equal(t, tc.wantGoal, policies[0].Goal)
		})
	}
}

func TestPolicies_TriggerCarried(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Reviewers: []config.ReviewerConfig{
			{
				Type:       "github-app",
				Name:       "coderabbitai",
				Goal:       config.GoalConfig{Approved: true},
				MaxRallies: 5,
				Trigger:    "@coderabbitai review",
			},
		},
	}

	policies, err := config.Policies(cfg)
	require.NoError(t, err)
	assert.Equal(t, "@coderabbitai review", policies[0].Trigger)
}

// --- Load / Init tests ---

// gitRepoWithConfig creates a fake git repo under a temp dir, optionally writing
// a config file at .github/<name>, and returns the directory from which config
// discovery should start (the repo root joined with workDir).
func gitRepoWithConfig(t *testing.T, name, content, workDir string) string {
	t.Helper()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))

	if name != "" {
		ghDir := filepath.Join(root, ".github")
		require.NoError(t, os.MkdirAll(ghDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(ghDir, name), []byte(content), 0o600))
	}

	start := root
	if workDir != "" {
		start = filepath.Join(root, workDir)
		require.NoError(t, os.MkdirAll(start, 0o755))
	}
	return start
}

func TestLoad_ValidFile(t *testing.T) {
	t.Parallel()

	content := `
reviewers:
  - type: github-copilot
    goal: { all-conversations-resolved: true }
`
	start := gitRepoWithConfig(t, "review-loop.yml", content, "")

	cfg, err := config.LoadFrom(start)
	require.NoError(t, err)
	require.Len(t, cfg.Reviewers, 1)
	assert.Equal(t, "github-copilot", cfg.Reviewers[0].Type)
}

func TestLoad_YamlExtensionAccepted(t *testing.T) {
	t.Parallel()

	content := `
reviewers:
  - type: github-copilot
    goal: { all-conversations-resolved: true }
`
	start := gitRepoWithConfig(t, "review-loop.yaml", content, "")

	cfg, err := config.LoadFrom(start)
	require.NoError(t, err)
	require.Len(t, cfg.Reviewers, 1)
}

func TestLoad_FoundFromSubdirectory(t *testing.T) {
	t.Parallel()

	content := `
reviewers:
  - type: github-copilot
    goal: { all-conversations-resolved: true }
`
	start := gitRepoWithConfig(t, "review-loop.yml", content, filepath.Join("pkg", "deep"))

	cfg, err := config.LoadFrom(start)
	require.NoError(t, err)
	require.Len(t, cfg.Reviewers, 1)
}

func TestLoad_MissingFile_ReturnsErrConfigNotFound(t *testing.T) {
	t.Parallel()

	start := gitRepoWithConfig(t, "", "", "")

	_, err := config.LoadFrom(start)
	require.Error(t, err)
	assert.ErrorIs(t, err, config.ErrConfigNotFound)
}

func TestLoad_NotInGitRepo_ReturnsErrConfigNotFound(t *testing.T) {
	t.Parallel()

	// A temp dir with no .git anywhere up the tree.
	_, err := config.LoadFrom(t.TempDir())
	require.Error(t, err)
	assert.ErrorIs(t, err, config.ErrConfigNotFound)
}

func TestInit_WritesParseableDefault(t *testing.T) {
	t.Parallel()

	start := gitRepoWithConfig(t, "", "", "")

	path, err := config.InitAt(start)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(path, filepath.Join(".github", "review-loop.yml")), "path: %s", path)

	// The written file must load cleanly.
	cfg, err := config.LoadFrom(start)
	require.NoError(t, err)
	require.NotEmpty(t, cfg.Reviewers)
}

func TestInit_RefusesToOverwrite(t *testing.T) {
	t.Parallel()

	start := gitRepoWithConfig(t, "review-loop.yml", "reviewers: []\n", "")

	_, err := config.InitAt(start)
	require.Error(t, err)
	assert.ErrorIs(t, err, config.ErrConfigExists)
}

func TestInit_NotInGitRepo_Errors(t *testing.T) {
	t.Parallel()

	_, err := config.InitAt(t.TempDir())
	require.Error(t, err)
}
