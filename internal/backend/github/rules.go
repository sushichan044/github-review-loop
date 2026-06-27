package github

import (
	"context"
	"errors"
	"fmt"
	"net/url"

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
//
// go-gh's REST helpers take no per-request context, so ctx cannot be propagated
// into the HTTP layer without disproportionate plumbing for this lazy view-only
// path. Cancellation is instead honored cheaply by checking ctx before each call.
func fetchBranchRules(ctx context.Context, pr backend.PRCoords) ([]backend.BranchRule, error) {
	restClient, err := api.DefaultRESTClient()
	if err != nil {
		return nil, fmt.Errorf("github rules: cannot create REST client: %w", err)
	}

	if err = ctx.Err(); err != nil {
		return nil, err
	}

	// The branch-rules path needs the PR's base branch name, not the repo name.
	// Fetch it from the PR metadata before building the rules URL.
	var prMeta struct {
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	}
	prPath := fmt.Sprintf("repos/%s/%s/pulls/%d", pr.Owner, pr.Repo, pr.Number)
	if err = restClient.Get(prPath, &prMeta); err != nil {
		return nil, fmt.Errorf("github rules: cannot resolve base branch: %w", err)
	}
	baseBranch := prMeta.Base.Ref
	if baseBranch == "" {
		return nil, errors.New("github rules: PR response missing base.ref")
	}

	if err = ctx.Err(); err != nil {
		return nil, err
	}

	// Branch refs may contain "/" (e.g. "feature/foo"); escape so the path
	// segment is not split into extra path components.
	path := fmt.Sprintf("repos/%s/%s/rules/branches/%s", pr.Owner, pr.Repo, url.PathEscape(baseBranch))

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
