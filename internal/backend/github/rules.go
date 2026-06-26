package github

import (
	"context"
	"fmt"

	"github.com/cli/go-gh/v2/pkg/api"

	"github.com/sushichan044/mergeable-please/internal/backend"
)

// branchRuleResponse is the REST API response shape for one rule entry.
type branchRuleResponse struct {
	Type       string         `json:"type"`
	Parameters map[string]any `json:"parameters"`
}

// fetchBranchRules fetches the configured branch protection rules for the PR's
// base branch via the REST API. This is view-only: the API reports what rules
// are configured, NOT whether they currently pass or fail.
//
// Endpoint: GET /repos/{owner}/{repo}/rules/branches/{branch}
// This is only called lazily by `view --condition rules`.
func fetchBranchRules(_ context.Context, pr backend.PRCoords) ([]backend.BranchRule, error) {
	restClient, err := api.DefaultRESTClient()
	if err != nil {
		return nil, fmt.Errorf("github rules: cannot create REST client: %w", err)
	}

	path := fmt.Sprintf("repos/%s/%s/rules/branches/%s", pr.Owner, pr.Repo, pr.Repo)

	var raw []branchRuleResponse
	if getErr := restClient.Get(path, &raw); getErr != nil {
		return nil, fmt.Errorf("github rules: fetch failed: %w", getErr)
	}

	rules := make([]backend.BranchRule, 0, len(raw))
	for _, r := range raw {
		rules = append(rules, backend.BranchRule{
			Type:       r.Type,
			Parameters: r.Parameters,
		})
	}
	return rules, nil
}
