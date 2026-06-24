package github

import (
	"strings"

	"github.com/sushichan044/github-review-loop/internal/reviewloop"
)

// ResolveIdentity maps a GitHub login to a [reviewloop.ReviewerIdentity] given
// the active policies. Returns the matched identity and true, or the zero value
// and false if no policy claims this login.
//
// Matching rules per reviewer type:
//   - "user": login == policy.Identity.Name (case-insensitive)
//   - "github-copilot": login ∈ {"copilot", "copilot-pull-request-reviewer"} (case-insensitive)
//   - "github-app": login == policy.Identity.Name OR login == policy.Identity.Name+"[bot]" (case-insensitive)
func ResolveIdentity(login string, policies []reviewloop.Policy) (reviewloop.ReviewerIdentity, bool) {
	for _, p := range policies {
		switch p.Identity.Type {
		case reviewloop.ReviewerTypeUser:
			if strings.EqualFold(login, p.Identity.Name) {
				return p.Identity, true
			}

		case reviewloop.ReviewerTypeGitHubCopilot:
			if isCopilotLogin(login) {
				return p.Identity, true
			}

		case reviewloop.ReviewerTypeGitHubApp:
			name := p.Identity.Name
			if strings.EqualFold(login, name) || strings.EqualFold(login, name+"[bot]") {
				return p.Identity, true
			}
		}
	}
	return reviewloop.ReviewerIdentity{}, false
}

// isCopilotLogin reports whether login is one of the well-known Copilot bot logins.
func isCopilotLogin(login string) bool {
	return strings.EqualFold(login, "copilot") ||
		strings.EqualFold(login, "copilot-pull-request-reviewer")
}
