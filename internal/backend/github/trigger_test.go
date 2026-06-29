package github_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sushichan044/mergeable-please/internal/backend/github"
	"github.com/sushichan044/mergeable-please/internal/core/reviewer"
)

// TestNewTriggererSignature verifies NewTriggerer is callable and returns a non-nil value.
// This also keeps deadcode analysis happy: the production constructor is reachable under -test.
func TestNewTriggererSignature(t *testing.T) {
	t.Parallel()
	tr := github.NewTriggerer()
	if tr == nil {
		t.Fatal("expected non-nil Triggerer")
	}
}

func TestTriggerer_RequestReview(t *testing.T) {
	t.Parallel()

	pr := github.PR{Owner: "myorg", Repo: "myrepo", Number: 42}

	tests := []struct {
		name     string
		policy   reviewer.Policy
		wantArgs []string
		wantErr  bool
	}{
		{
			name: "user reviewer uses add-reviewer with name",
			policy: reviewer.Policy{
				Identity: reviewer.Identity{
					Type: reviewer.ReviewerTypeUser,
					Name: "alice",
				},
			},
			wantArgs: []string{"pr", "edit", "42", "--repo", "myorg/myrepo", "--add-reviewer", "alice"},
		},
		{
			name: "github-copilot reviewer uses add-reviewer @copilot",
			policy: reviewer.Policy{
				Identity: reviewer.Identity{
					Type: reviewer.ReviewerTypeGitHubCopilot,
					Name: "copilot",
				},
			},
			wantArgs: []string{"pr", "edit", "42", "--repo", "myorg/myrepo", "--add-reviewer", "@copilot"},
		},
		{
			name: "github-app reviewer without trigger uses add-reviewer with name",
			policy: reviewer.Policy{
				Identity: reviewer.Identity{
					Type: reviewer.ReviewerTypeGitHubApp,
					Name: "coderabbitai",
				},
			},
			wantArgs: []string{"pr", "edit", "42", "--repo", "myorg/myrepo", "--add-reviewer", "coderabbitai"},
		},
		{
			name: "github-app reviewer with trigger posts comment",
			policy: reviewer.Policy{
				Identity: reviewer.Identity{
					Type: reviewer.ReviewerTypeGitHubApp,
					Name: "coderabbitai",
				},
				Trigger: "@coderabbitai review",
			},
			wantArgs: []string{"pr", "comment", "42", "--repo", "myorg/myrepo", "--body", "@coderabbitai review"},
		},
		{
			name: "user reviewer with trigger uses comment path",
			policy: reviewer.Policy{
				Identity: reviewer.Identity{
					Type: reviewer.ReviewerTypeUser,
					Name: "alice",
				},
				Trigger: "/review please",
			},
			wantArgs: []string{"pr", "comment", "42", "--repo", "myorg/myrepo", "--body", "/review please"},
		},
		{
			name: "github-copilot with trigger uses comment path",
			policy: reviewer.Policy{
				Identity: reviewer.Identity{
					Type: reviewer.ReviewerTypeGitHubCopilot,
					Name: "copilot",
				},
				Trigger: "@copilot review",
			},
			wantArgs: []string{"pr", "comment", "42", "--repo", "myorg/myrepo", "--body", "@copilot review"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var capturedArgs []string
			fakeExec := func(args ...string) (bytes.Buffer, bytes.Buffer, error) {
				capturedArgs = args
				return bytes.Buffer{}, bytes.Buffer{}, nil
			}

			tr := github.NewTriggererWithExec(fakeExec)
			err := tr.RequestReview(pr, tc.policy)

			require.NoError(t, err)
			assert.Equal(t, tc.wantArgs, capturedArgs)
		})
	}
}

func TestTriggerer_RequestReview_ExecError(t *testing.T) {
	t.Parallel()

	pr := github.PR{Owner: "myorg", Repo: "myrepo", Number: 1}
	policy := reviewer.Policy{
		Identity: reviewer.Identity{
			Type: reviewer.ReviewerTypeUser,
			Name: "alice",
		},
	}

	fakeErr := errors.New("gh: authentication required")
	fakeExec := func(_ ...string) (bytes.Buffer, bytes.Buffer, error) {
		return bytes.Buffer{}, bytes.Buffer{}, fakeErr
	}

	tr := github.NewTriggererWithExec(fakeExec)
	err := tr.RequestReview(pr, policy)

	require.Error(t, err)
	assert.ErrorIs(t, err, fakeErr)
}
