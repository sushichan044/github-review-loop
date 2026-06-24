package github_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sushichan044/github-review-loop/internal/github"
)

func TestParsePRArg(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		arg        string
		wantOwner  string
		wantRepo   string
		wantNumber int
		wantErrMsg string
	}{
		{
			name:       "bare integer",
			arg:        "42",
			wantOwner:  "",
			wantRepo:   "",
			wantNumber: 42,
		},
		{
			name:       "full GitHub URL",
			arg:        "https://github.com/owner/repo/pull/123",
			wantOwner:  "owner",
			wantRepo:   "repo",
			wantNumber: 123,
		},
		{
			name:       "full GitHub URL with trailing slash",
			arg:        "https://github.com/owner/repo/pull/99/",
			wantOwner:  "owner",
			wantRepo:   "repo",
			wantNumber: 99,
		},
		{
			name:       "GitHub Enterprise style URL",
			arg:        "https://github.example.com/myorg/myrepo/pull/7",
			wantOwner:  "myorg",
			wantRepo:   "myrepo",
			wantNumber: 7,
		},
		{
			name:       "empty string",
			arg:        "",
			wantErrMsg: "must not be empty",
		},
		{
			name:       "zero integer",
			arg:        "0",
			wantErrMsg: "must be positive",
		},
		{
			name:       "negative integer",
			arg:        "-5",
			wantErrMsg: "must be positive",
		},
		{
			name:       "plain string",
			arg:        "notanumber",
			wantErrMsg: "invalid PR number or URL",
		},
		{
			name:       "URL without pull segment",
			arg:        "https://github.com/owner/repo/issues/5",
			wantErrMsg: "invalid PR number or URL",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			owner, repo, number, err := github.ParsePRArg(tc.arg)
			if tc.wantErrMsg != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantOwner, owner)
			assert.Equal(t, tc.wantRepo, repo)
			assert.Equal(t, tc.wantNumber, number)
		})
	}
}

// TestGHPRResolverCurrentPR verifies GHPRResolver.CurrentPR is callable.
// It requires a live gh CLI and an active PR branch, so it skips when unavailable.
func TestGHPRResolverCurrentPR(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("requires gh CLI and an active PR branch")
	}
	resolver := github.GHPRResolver{}
	_, _, _, err := resolver.CurrentPR(context.Background())
	if err != nil {
		t.Skipf("no active PR branch: %v", err)
	}
}
