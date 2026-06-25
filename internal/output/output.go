// Package output renders the review-loop state for human or agent consumption.
// It is pure rendering: it changes representation only, never state, guard, or trigger behavior.
package output

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jehiah/agentdetection"

	"github.com/sushichan044/github-review-loop/internal/reviewloop"
)

const programName = "github-review-loop"

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
	Author   string
	Body     string
	URL      string
	At       time.Time
	Resolved bool
}

// ReviewerView is the complete rendering context for one reviewer.
// MaxRallies is populated by the caller (Task 7) from reviewloop.Policy
// because ReviewerState does not carry it.
type ReviewerView struct {
	Identity           reviewloop.ReviewerIdentity
	Goal               reviewloop.Goal
	Phase              reviewloop.Phase
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
	case reviewloop.PhaseGoalMet:
		return "Goal met — nothing to do for this reviewer."

	case reviewloop.PhaseExhausted:
		return fmt.Sprintf(
			"WARNING: exhausted %d/%d rallies without meeting goal; stop or raise max-rallies.",
			r.RallyCount, r.MaxRallies,
		)

	case reviewloop.PhaseActive:
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
		if r.Phase == reviewloop.PhaseExhausted {
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
	case reviewloop.PhaseGoalMet:
		return "Goal met — nothing to do for this reviewer."

	case reviewloop.PhaseExhausted:
		return fmt.Sprintf(
			"**WARNING:** exhausted %d/%d rallies without meeting goal. "+
				"Stop the loop or raise `max-rallies` in configuration.",
			r.RallyCount, r.MaxRallies,
		)

	case reviewloop.PhaseActive:
		if r.CanRerequest {
			return fmt.Sprintf(
				"Ready to (re)request review.\n\n"+
					"Run: `%s request --reviewer %s`\n\n"+
					"After requesting, wait using a background shell rather than blocking, for example:\n"+
					"```sh\nsleep 60 && %s status\n```\n"+
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
		if r.Phase == reviewloop.PhaseExhausted {
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

// ── Helpers ──────────────────────────────────────────────────────────────────

// formatIdentity returns the canonical "type:name" string for a reviewer identity.
func formatIdentity(id reviewloop.ReviewerIdentity) string {
	if id.Name == "" {
		return string(id.Type)
	}

	return fmt.Sprintf("%s:%s", id.Type, id.Name)
}
