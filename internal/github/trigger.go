package github

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/cli/go-gh/v2"

	"github.com/sushichan044/github-review-loop/internal/reviewloop"
)

// copilotReviewHandle is the reviewer handle accepted by `gh pr edit --add-reviewer`
// for GitHub Copilot. This is distinct from the event login ("copilot" /
// "copilot-pull-request-reviewer") that appears in timeline and review events.
const copilotReviewHandle = "@copilot"

// ghExecFunc is a testable seam for the gh CLI execution function.
// Its signature matches [gh.Exec].
type ghExecFunc func(args ...string) (stdout, stderr bytes.Buffer, err error)

// Triggerer fires the re-request action for a single reviewer on a PR.
// The production default uses [gh.Exec]; tests inject a fake via [NewTriggerer].
type Triggerer struct {
	exec ghExecFunc
}

// NewTriggerer returns a [Triggerer] backed by the real gh CLI.
func NewTriggerer() *Triggerer {
	return &Triggerer{exec: gh.Exec}
}

// NewTriggererWithExec returns a [Triggerer] that uses the provided exec function.
// Intended for use in tests.
func NewTriggererWithExec(fn ghExecFunc) *Triggerer {
	return &Triggerer{exec: fn}
}

// RequestReview fires the appropriate gh command to re-request a review
// according to the given policy. It does NOT check whether a re-request is
// allowed (CanRerequest) — that guard lives in core (Task 7).
//
// Dispatch rules:
//   - If policy.Trigger != "" → post the trigger string as a PR comment (any type).
//   - ReviewerTypeUser        → gh pr edit --add-reviewer <name>
//   - ReviewerTypeGitHubCopilot → gh pr edit --add-reviewer @copilot
//   - ReviewerTypeGitHubApp   → gh pr edit --add-reviewer <name>
func (t *Triggerer) RequestReview(pr PR, policy reviewloop.Policy) error {
	repo := pr.Owner + "/" + pr.Repo
	number := strconv.Itoa(pr.Number)

	if policy.Trigger != "" {
		_, _, err := t.exec("pr", "comment", number, "--repo", repo, "--body", policy.Trigger)
		if err != nil {
			return fmt.Errorf("failed to post trigger comment for %s: %w", policy.Identity.Name, err)
		}
		return nil
	}

	var handle string
	switch policy.Identity.Type {
	case reviewloop.ReviewerTypeGitHubCopilot:
		handle = copilotReviewHandle
	case reviewloop.ReviewerTypeUser, reviewloop.ReviewerTypeGitHubApp:
		handle = policy.Identity.Name
	}

	_, _, err := t.exec("pr", "edit", number, "--repo", repo, "--add-reviewer", handle)
	if err != nil {
		return fmt.Errorf("failed to request review from %s: %w", policy.Identity.Name, err)
	}
	return nil
}
