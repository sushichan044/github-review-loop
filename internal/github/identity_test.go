package github_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sushichan044/github-review-loop/internal/github"
	"github.com/sushichan044/github-review-loop/internal/reviewloop"
)

func TestResolveIdentity(t *testing.T) {
	t.Parallel()

	policies := []reviewloop.Policy{
		{
			Identity: reviewloop.ReviewerIdentity{
				Type: reviewloop.ReviewerTypeUser,
				Name: "alice",
			},
		},
		{
			Identity: reviewloop.ReviewerIdentity{
				Type: reviewloop.ReviewerTypeGitHubCopilot,
			},
		},
		{
			Identity: reviewloop.ReviewerIdentity{
				Type: reviewloop.ReviewerTypeGitHubApp,
				Name: "my-bot",
			},
		},
	}

	tests := []struct {
		name      string
		login     string
		wantFound bool
		wantType  reviewloop.ReviewerType
	}{
		{
			name:      "user exact match",
			login:     "alice",
			wantFound: true,
			wantType:  reviewloop.ReviewerTypeUser,
		},
		{
			name:      "user case-insensitive",
			login:     "ALICE",
			wantFound: true,
			wantType:  reviewloop.ReviewerTypeUser,
		},
		{
			name:      "copilot login",
			login:     "copilot",
			wantFound: true,
			wantType:  reviewloop.ReviewerTypeGitHubCopilot,
		},
		{
			name:      "copilot-pull-request-reviewer login",
			login:     "copilot-pull-request-reviewer",
			wantFound: true,
			wantType:  reviewloop.ReviewerTypeGitHubCopilot,
		},
		{
			name:      "copilot case-insensitive",
			login:     "Copilot",
			wantFound: true,
			wantType:  reviewloop.ReviewerTypeGitHubCopilot,
		},
		{
			name:      "github-app exact match",
			login:     "my-bot",
			wantFound: true,
			wantType:  reviewloop.ReviewerTypeGitHubApp,
		},
		{
			name:      "github-app with [bot] suffix",
			login:     "my-bot[bot]",
			wantFound: true,
			wantType:  reviewloop.ReviewerTypeGitHubApp,
		},
		{
			name:      "github-app [bot] suffix case-insensitive",
			login:     "MY-BOT[BOT]",
			wantFound: true,
			wantType:  reviewloop.ReviewerTypeGitHubApp,
		},
		{
			name:      "unknown login returns false",
			login:     "nobody",
			wantFound: false,
		},
		{
			name:      "empty login returns false",
			login:     "",
			wantFound: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			identity, ok := github.ResolveIdentity(tc.login, policies)
			assert.Equal(t, tc.wantFound, ok)
			if tc.wantFound {
				assert.Equal(t, tc.wantType, identity.Type)
			}
		})
	}
}

func TestResolveIdentity_EmptyPolicies(t *testing.T) {
	t.Parallel()
	_, ok := github.ResolveIdentity("alice", nil)
	assert.False(t, ok)
}
