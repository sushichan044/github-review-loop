package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/cli/go-gh/v2"
	"github.com/cli/go-gh/v2/pkg/repository"
)

// PR holds the coordinates of a GitHub pull request.
type PR struct {
	Owner  string
	Repo   string
	Number int
}

// PRResolver resolves the PR from the current branch context.
// The real implementation uses gh CLI; tests may substitute a fake.
type PRResolver interface {
	CurrentPR(ctx context.Context) (owner, repo string, number int, err error)
}

// GHPRResolver is the real [PRResolver] that uses the gh CLI.
type GHPRResolver struct{}

// CurrentPR detects the PR for the current git branch via gh CLI.
func (GHPRResolver) CurrentPR(_ context.Context) (string, string, int, error) {
	stdout, _, execErr := gh.Exec("pr", "view", "--json", "number")
	if execErr != nil {
		return "", "", 0, fmt.Errorf("no PR found for current branch: %w", execErr)
	}

	var result struct {
		Number int `json:"number"`
	}
	if parseErr := json.Unmarshal(stdout.Bytes(), &result); parseErr != nil {
		return "", "", 0, fmt.Errorf("failed to parse PR info: %w", parseErr)
	}
	if result.Number == 0 {
		return "", "", 0, errors.New("no PR found for current branch")
	}

	repo, repoErr := repository.Current()
	if repoErr != nil {
		return "", "", 0, fmt.Errorf("failed to determine repository: %w", repoErr)
	}
	return repo.Owner, repo.Name, result.Number, nil
}

// ParsePRArg parses a PR argument in one of these forms:
//   - bare integer: uses current repo (caller must supply owner/repo from context)
//   - full GitHub URL containing /pull/<number>
//
// For bare integers this function returns owner="" repo="" — the caller must
// fill them in from the current repository context.
// Returns an error for empty or unrecognisable inputs.
func ParsePRArg(arg string) (string, string, int, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", "", 0, errors.New("PR argument must not be empty")
	}

	// Bare integer — repo context must come from elsewhere.
	if n, convErr := strconv.Atoi(arg); convErr == nil {
		if n <= 0 {
			return "", "", 0, fmt.Errorf("PR number must be positive, got %d", n)
		}
		return "", "", n, nil
	}

	// URL containing /{owner}/{repo}/pull/{number}
	u, parseErr := url.Parse(arg)
	if parseErr == nil && u.Host != "" {
		parts := strings.Split(path.Clean(u.Path), "/")
		// parts after Clean: ["", owner, repo, "pull", number, ...]
		for i, p := range parts {
			if p == "pull" && i+1 < len(parts) && i >= 3 {
				if n, convErr := strconv.Atoi(parts[i+1]); convErr == nil {
					return parts[i-2], parts[i-1], n, nil
				}
			}
		}
	}

	return "", "", 0, fmt.Errorf("invalid PR number or URL: %s", arg)
}
