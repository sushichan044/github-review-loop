// Package output renders the review-loop state for human or agent consumption.
// It is pure rendering: it changes representation only, never state, guard, or trigger behavior.
package output

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jehiah/agentdetection"

	"github.com/sushichan044/mergeable-please/internal/core"
	"github.com/sushichan044/mergeable-please/internal/core/reviewer"
)

const programName = "mergeable-please"

// Format selects the output representation.
type Format string

const (
	// FormatAgent produces verbose markdown output suitable for AI agents.
	FormatAgent Format = "agent"
	// FormatHuman produces concise plain-text output suitable for humans.
	FormatHuman Format = "human"
)

// ParseFormat converts a string to a Format. It returns an error for unknown values.
func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case FormatAgent:
		return FormatAgent, nil
	case FormatHuman:
		return FormatHuman, nil
	default:
		return "", fmt.Errorf("unknown format %q: must be %q or %q", s, FormatAgent, FormatHuman)
	}
}

// FormatFor returns the appropriate Format given whether the caller is an AI agent.
// This is the testable pure-function core of format detection.
func FormatFor(isAgent bool) Format {
	if isAgent {
		return FormatAgent
	}

	return FormatHuman
}

// DefaultFormat returns the default output format, detecting agent vs. human automatically.
func DefaultFormat() Format {
	return FormatFor(agentdetection.IsAgent())
}

// CommentView is a rendered snapshot of a single review comment.
type CommentView struct {
	Author string
	Body   string
	URL    string
	At     time.Time
}

// ReviewerView is the complete rendering context for one reviewer.
// MaxRallies is populated by the caller (Task 7) from reviewer.Policy
// because State does not carry it.
type ReviewerView struct {
	Identity           reviewer.Identity
	Goal               reviewer.Goal
	Phase              reviewer.Phase
	RallyCount         int
	MaxRallies         int
	GoalMet            bool
	CanRerequest       bool
	BlockReason        string
	UnresolvedComments []CommentView
	NewComments        []CommentView
}

// LoopView is the complete rendering context for the entire review loop.
type LoopView struct {
	Reviewers []ReviewerView
	Done      bool
}

// Render writes v to w in the chosen format.
// Reviewers are rendered in the order they appear in v.Reviewers (stable, caller-controlled).
func Render(w io.Writer, v LoopView, f Format) error {
	switch f {
	case FormatAgent:
		return renderAgent(w, v)
	case FormatHuman:
		return renderHuman(w, v)
	default:
		return fmt.Errorf("unsupported format %q", f)
	}
}

// ── Human format ────────────────────────────────────────────────────────────

func renderHuman(w io.Writer, v LoopView) error {
	for i, r := range v.Reviewers {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}

		if err := renderReviewerHuman(w, r); err != nil {
			return err
		}
	}

	if v.Done {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}

		if err := renderLoopDoneHuman(w, v); err != nil {
			return err
		}
	}

	return nil
}

func renderReviewerHuman(w io.Writer, r ReviewerView) error {
	id := formatIdentity(r.Identity)
	lines := []string{
		fmt.Sprintf("Reviewer: %s", id),
		fmt.Sprintf("Phase:    %s", r.Phase),
		fmt.Sprintf("Rally:    %d/%d", r.RallyCount, r.MaxRallies),
		fmt.Sprintf("Goal:     %s (met: %v)", r.Goal, r.GoalMet),
	}

	if len(r.UnresolvedComments) > 0 {
		lines = append(lines, fmt.Sprintf("Unresolved comments (%d):", len(r.UnresolvedComments)))
		for _, c := range r.UnresolvedComments {
			lines = append(lines, fmt.Sprintf("  - [%s] %s", c.Author, c.Body))
		}
	}

	if len(r.NewComments) > 0 {
		lines = append(lines, fmt.Sprintf("New comments since last rally (%d):", len(r.NewComments)))
		for _, c := range r.NewComments {
			lines = append(lines, fmt.Sprintf("  - [%s] %s", c.Author, c.Body))
		}
	}

	lines = append(lines, fmt.Sprintf("Next action: %s", nextActionHuman(r)))

	for _, l := range lines {
		if _, err := fmt.Fprintln(w, l); err != nil {
			return err
		}
	}

	return nil
}

func nextActionHuman(r ReviewerView) string {
	switch r.Phase {
	case reviewer.PhaseGoalMet:
		return "Goal met — nothing to do for this reviewer."

	case reviewer.PhaseExhausted:
		return fmt.Sprintf(
			"WARNING: exhausted %d/%d rallies without meeting goal; stop or raise max-rallies.",
			r.RallyCount, r.MaxRallies,
		)

	case reviewer.PhaseActive:
		if r.CanRerequest {
			return fmt.Sprintf(
				"Ready to (re)request review: run `%s request --reviewer %s`.",
				programName, formatIdentity(r.Identity),
			)
		}

		return fmt.Sprintf(
			"Re-request blocked (%s). Address unresolved comments and push a new commit before re-requesting.",
			r.BlockReason,
		)
	}

	// unreachable: all Phase values handled above
	return ""
}

func renderLoopDoneHuman(w io.Writer, v LoopView) error {
	var exhausted []string
	for _, r := range v.Reviewers {
		if r.Phase == reviewer.PhaseExhausted {
			exhausted = append(exhausted, formatIdentity(r.Identity))
		}
	}

	msg := "Loop complete."
	if len(exhausted) > 0 {
		msg += fmt.Sprintf(
			" Warning: the following reviewers exhausted their rallies without meeting the goal: %s.",
			strings.Join(exhausted, ", "),
		)
	}

	_, err := fmt.Fprintln(w, msg)

	return err
}

// ── Agent format ─────────────────────────────────────────────────────────────

func renderAgent(w io.Writer, v LoopView) error {
	for i, r := range v.Reviewers {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}

		if err := renderReviewerAgent(w, r); err != nil {
			return err
		}
	}

	if v.Done {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}

		if err := renderLoopDoneAgent(w, v); err != nil {
			return err
		}
	}

	return nil
}

func renderReviewerAgent(w io.Writer, r ReviewerView) error {
	id := formatIdentity(r.Identity)
	sections := []string{
		fmt.Sprintf("## Reviewer: `%s`", id),
		fmt.Sprintf("- **Phase:** %s", r.Phase),
		fmt.Sprintf("- **Rally:** %d/%d", r.RallyCount, r.MaxRallies),
		fmt.Sprintf("- **Goal:** `%s` (met: %v)", r.Goal, r.GoalMet),
	}

	if len(r.UnresolvedComments) > 0 {
		sections = append(sections, fmt.Sprintf("\n### Unresolved comments (%d)", len(r.UnresolvedComments)))

		for _, c := range r.UnresolvedComments {
			sections = append(sections, fmt.Sprintf("- **%s:** %s", c.Author, c.Body))

			if c.URL != "" {
				sections = append(sections, fmt.Sprintf("  <%s>", c.URL))
			}
		}
	}

	if len(r.NewComments) > 0 {
		sections = append(sections, fmt.Sprintf("\n### New comments since last rally (%d)", len(r.NewComments)))

		for _, c := range r.NewComments {
			sections = append(sections, fmt.Sprintf("- **%s** (%s): %s", c.Author, c.At.Format(time.RFC3339), c.Body))

			if c.URL != "" {
				sections = append(sections, fmt.Sprintf("  <%s>", c.URL))
			}
		}
	}

	sections = append(sections, fmt.Sprintf("\n### Next action\n%s", nextActionAgent(r)))

	for _, s := range sections {
		if _, err := fmt.Fprintln(w, s); err != nil {
			return err
		}
	}

	return nil
}

func nextActionAgent(r ReviewerView) string {
	switch r.Phase {
	case reviewer.PhaseGoalMet:
		return "Goal met — nothing to do for this reviewer."

	case reviewer.PhaseExhausted:
		return fmt.Sprintf(
			"**WARNING:** exhausted %d/%d rallies without meeting goal. "+
				"Stop the loop or raise `max-rallies` in configuration.",
			r.RallyCount, r.MaxRallies,
		)

	case reviewer.PhaseActive:
		if r.CanRerequest {
			return fmt.Sprintf(
				"Ready to (re)request review.\n\n"+
					"Run: `%s request --reviewer %s`\n\n"+
					"After requesting, wait using a background shell rather than blocking, for example:\n"+
					"```sh\nsleep 60 && %s check\n```\n"+
					"Then re-check the status.",
				programName, formatIdentity(r.Identity), programName,
			)
		}

		return fmt.Sprintf(
			"Re-request is blocked: %s\n\n"+
				"Address the unresolved comments above and push a new commit before re-requesting.",
			r.BlockReason,
		)
	}

	// unreachable: all Phase values handled above
	return ""
}

func renderLoopDoneAgent(w io.Writer, v LoopView) error {
	var exhausted []string
	for _, r := range v.Reviewers {
		if r.Phase == reviewer.PhaseExhausted {
			exhausted = append(exhausted, fmt.Sprintf("`%s`", formatIdentity(r.Identity)))
		}
	}

	var sb strings.Builder
	sb.WriteString("---\n## Loop complete\n\n")

	if len(exhausted) > 0 {
		fmt.Fprintf(
			&sb,
			"**Warning:** the following reviewers exhausted their rallies without meeting the goal: %s.\n",
			strings.Join(exhausted, ", "),
		)
	} else {
		sb.WriteString("All reviewers reached their goal.\n")
	}

	_, err := fmt.Fprint(w, sb.String())

	return err
}

// ── CheckResult rendering ────────────────────────────────────────────────────

// RenderCheckResult writes a [core.CheckResult] to w in the chosen format.
// It always emits:
//   - a "status: satisfied|blocked" line
//   - the Blockers section (when non-empty)
//   - the Advisories section (always, even when satisfied)
//   - the reviewer-loop subsection (when ReviewerLoop is non-nil)
func RenderCheckResult(w io.Writer, r core.CheckResult, f Format) error {
	switch f {
	case FormatAgent:
		return renderCheckAgent(w, r)
	case FormatHuman:
		return renderCheckHuman(w, r)
	default:
		return fmt.Errorf("unsupported format %q", f)
	}
}

// checkRenderStyles holds the format strings that differ between human and agent output.
type checkRenderStyles struct {
	blockersHeader     string
	blockerItem        string // args: kind, title
	blockerDetail      string // args: detail
	blockerAction      string // args: action
	blockerDrillIn     string // args: drillIn
	advisoriesHeader   string
	advisoryItem       string // args: kind, title
	advisoryDetail     string // args: detail
	reviewerLoopHeader string
}

func humanCheckStyles() checkRenderStyles {
	return checkRenderStyles{
		blockersHeader:     "\nBlockers:",
		blockerItem:        "  [%s] %s\n",
		blockerDetail:      "    %s\n",
		blockerAction:      "    Action: %s\n",
		blockerDrillIn:     "    Detail: %s\n",
		advisoriesHeader:   "\nAdvisories (require human action):",
		advisoryItem:       "  [%s] %s\n",
		advisoryDetail:     "    %s\n",
		reviewerLoopHeader: "\nReviewer loop:",
	}
}

func agentCheckStyles() checkRenderStyles {
	return checkRenderStyles{
		blockersHeader:     "\n## Blockers",
		blockerItem:        "\n### [%s] %s\n",
		blockerDetail:      "- **Detail:** %s\n",
		blockerAction:      "- **Action:** %s\n",
		blockerDrillIn:     "- **Drill in:** `%s`\n",
		advisoriesHeader:   "\n## Advisories (require human action)",
		advisoryItem:       "\n### [%s] %s\n",
		advisoryDetail:     "- **Detail:** %s\n",
		reviewerLoopHeader: "\n## Reviewer loop",
	}
}

func renderCheckHuman(w io.Writer, r core.CheckResult) error {
	return renderCheck(w, r, humanCheckStyles())
}

func renderCheckAgent(w io.Writer, r core.CheckResult) error {
	return renderCheck(w, r, agentCheckStyles())
}

func renderCheck(w io.Writer, r core.CheckResult, s checkRenderStyles) error {
	statusStr := "satisfied"
	if !r.Satisfied {
		statusStr = "blocked"
	}
	if _, err := fmt.Fprintf(w, "status: %s\n", statusStr); err != nil {
		return err
	}
	if err := writeBlockersSection(w, r.Blockers, s); err != nil {
		return err
	}
	if err := writeAdvisoriesSection(w, r.Advisories, s); err != nil {
		return err
	}
	if r.ReviewerLoop != nil {
		if _, err := fmt.Fprintln(w, s.reviewerLoopHeader); err != nil {
			return err
		}
	}
	return nil
}

func writeBlockersSection(w io.Writer, blockers []core.Condition, s checkRenderStyles) error {
	if len(blockers) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, s.blockersHeader); err != nil {
		return err
	}
	for _, c := range blockers {
		if err := writeBlocker(w, c, s); err != nil {
			return err
		}
	}
	return nil
}

func writeAdvisoriesSection(w io.Writer, advisories []core.Condition, s checkRenderStyles) error {
	if len(advisories) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, s.advisoriesHeader); err != nil {
		return err
	}
	for _, c := range advisories {
		if err := writeAdvisory(w, c, s); err != nil {
			return err
		}
	}
	return nil
}

func writeBlocker(w io.Writer, c core.Condition, s checkRenderStyles) error {
	if _, err := fmt.Fprintf(w, s.blockerItem, c.Kind, c.Title); err != nil {
		return err
	}
	if c.Detail != "" {
		if _, err := fmt.Fprintf(w, s.blockerDetail, c.Detail); err != nil {
			return err
		}
	}
	if c.SuggestedAction != "" {
		if _, err := fmt.Fprintf(w, s.blockerAction, c.SuggestedAction); err != nil {
			return err
		}
	}
	if c.DrillInCmd != "" {
		if _, err := fmt.Fprintf(w, s.blockerDrillIn, c.DrillInCmd); err != nil {
			return err
		}
	}
	return nil
}

func writeAdvisory(w io.Writer, c core.Condition, s checkRenderStyles) error {
	if _, err := fmt.Fprintf(w, s.advisoryItem, c.Kind, c.Title); err != nil {
		return err
	}
	if c.Detail != "" {
		if _, err := fmt.Fprintf(w, s.advisoryDetail, c.Detail); err != nil {
			return err
		}
	}
	return nil
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// formatIdentity returns the canonical "type:name" string for a reviewer identity.
func formatIdentity(id reviewer.Identity) string {
	if id.Name == "" {
		return string(id.Type)
	}

	return fmt.Sprintf("%s:%s", id.Type, id.Name)
}
