// Package backend defines the interface every VCS backend must satisfy and the
// shared value types used across backends.
package backend

import (
	"context"

	"github.com/sushichan044/mergeable-please/internal/core"
)

// PRCoords uniquely identifies a pull request.
type PRCoords struct {
	Owner  string
	Repo   string
	Number int
}

// BranchRule describes one configured rule in the branch ruleset returned by
// FetchBranchRules. It is view-only data: the API reports what rules exist, not
// whether they pass or fail.
type BranchRule struct {
	// Type is the rule type string from the API (e.g. "pull_request", "required_status_checks").
	Type string
	// Parameters holds the raw rule parameters as a map for human display.
	Parameters map[string]any
}

// Backend is the contract every VCS integration must satisfy.
// All methods are called at most once per `check` invocation except
// FetchBranchRules, which is called only by `view --condition rules`.
type Backend interface {
	// BundledEvaluate fetches all merge-readiness signals in a single round-trip
	// and returns a CheckResult with Blockers and Advisories populated.
	// The caller is responsible for calling CheckResult.Finalize after optionally
	// attaching ReviewerLoop.
	BundledEvaluate(ctx context.Context, pr PRCoords) (core.CheckResult, error)

	// FetchBranchRules returns the configured rules for the PR's base branch.
	// This is a lazy REST call used only by `view --condition rules`.
	// Backends that do not support this may return nil, nil.
	FetchBranchRules(ctx context.Context, pr PRCoords) ([]BranchRule, error)
}

// ConflictChecker is an optional seam for non-GitHub backends to check for
// merge conflicts locally (e.g., via go-git). The GitHub backend does not use
// this interface because it reads mergeStateStatus from the GraphQL response.
//
// Future backends can implement this to provide a local-first conflict check
// when the remote API does not expose merge-state information.
type ConflictChecker interface {
	CheckConflicts(ctx context.Context, pr PRCoords) (bool, error)
}
