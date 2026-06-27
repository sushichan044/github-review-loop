// Package output renders merge-readiness results as Markdown for both humans and
// AI agents. It is pure rendering: it changes representation only, never state,
// guard, or trigger behavior.
package output

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sushichan044/mergeable-please/internal/core"
	"github.com/sushichan044/mergeable-please/internal/core/reviewer"
)

const programName = "mergeable-please"

// CommentView is a rendered snapshot of a single review comment.
type CommentView struct {
	Author string
	Body   string
	URL    string
	At     time.Time
}

// ReviewerView is the rendering context for one reviewer. MaxRallies is populated
// by the caller from reviewer.Policy because reviewer.State does not carry it.
//
// Two rendering modes are supported:
//   - Full mode (view --condition reviewers): populate UnresolvedComments with comment
//     bodies. UnresolvedCount and DrillInCmd are ignored.
//   - Concise mode (check): set UnresolvedCount and DrillInCmd; leave UnresolvedComments
//     empty. The renderer shows only the count + a drill-in command.
type ReviewerView struct {
	Identity     reviewer.Identity
	Goal         reviewer.Goal
	Phase        reviewer.Phase
	RallyCount   int
	MaxRallies   int
	GoalMet      bool
	CanRerequest bool
	BlockReason  string

	// Full mode (view --condition reviewers): comment bodies.
	UnresolvedComments []CommentView

	// Concise mode (check): count + drill-in command, no bodies.
	UnresolvedCount int    // number of unresolved review threads for this reviewer
	DrillInCmd      string // gh command to fetch this reviewer's unresolved review comments

	// ChangesRequested is true when the reviewer's latest review requests changes;
	// it drives the changes-requested next-action and gates the loop upstream.
	ChangesRequested bool

	// Latest-review summary, used to surface a review-level body (findings not
	// tied to any inline thread). LatestReviewState is the GitHub review state.
	LatestReviewState     reviewer.ReviewState
	LatestReviewCommitOID string

	// ReviewBodyPresent is true when the latest review has a non-empty body.
	// ReviewBodyDrillInCmd is the gh command to read that body; it is set only in
	// full mode (view). In concise mode (check) it is empty and the renderer
	// points the agent at `view --condition reviewers` instead.
	ReviewBodyPresent    bool
	ReviewBodyDrillInCmd string
}

// LoopView is the rendering context for the entire reviewer loop.
type LoopView struct {
	Reviewers []ReviewerView
	Done      bool
}

// RenderCheckResult writes a [core.CheckResult] as Markdown. It always emits a
// machine-readable "status: satisfied|blocked" line (the loop stop signal), then
// the Blockers, Advisories, and reviewer-loop sections when present.
func RenderCheckResult(w io.Writer, r core.CheckResult, loopView *LoopView) error {
	status := "satisfied"
	if !r.Satisfied {
		status = "blocked"
	}
	if _, err := fmt.Fprintf(w, "status: %s\n", status); err != nil {
		return err
	}

	if err := writeConditionsSection(w, "Blockers", r.Blockers); err != nil {
		return err
	}
	if err := writeConditionsSection(w, "Advisories (require human action)", r.Advisories); err != nil {
		return err
	}
	if loopView != nil {
		if err := writeReviewerLoop(w, *loopView); err != nil {
			return err
		}
	}
	return nil
}

// RenderDimensionView renders a filtered set of conditions for a single dimension
// (e.g. view --condition checks). It emits NO global "status:" verdict — only the
// matching conditions — because a partial view that omits the reviewer loop must
// not imply an authoritative pass/fail.
func RenderDimensionView(w io.Writer, blockers, advisories []core.Condition) error {
	if len(blockers) == 0 && len(advisories) == 0 {
		_, err := fmt.Fprintln(w, "No conditions found for this dimension.")
		return err
	}
	if err := writeConditionsSection(w, "Blockers", blockers); err != nil {
		return err
	}
	return writeConditionsSection(w, "Advisories (require human action)", advisories)
}

// Render writes the full reviewer-loop view as Markdown (view --condition reviewers),
// including comment bodies.
func Render(w io.Writer, v LoopView) error {
	return writeReviewerLoop(w, v)
}

// writeConditionsSection writes a "## <header>" section with one "### [kind] title"
// subsection per condition. It writes nothing when conditions is empty.
func writeConditionsSection(w io.Writer, header string, conditions []core.Condition) error {
	if len(conditions) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(w, "\n## %s\n", header); err != nil {
		return err
	}
	for _, c := range conditions {
		if _, err := fmt.Fprintf(w, "\n### [%s] %s\n", c.Kind, c.Title); err != nil {
			return err
		}
		if c.Detail != "" {
			if _, err := fmt.Fprintf(w, "- **Detail:** %s\n", c.Detail); err != nil {
				return err
			}
		}
		if c.SuggestedAction != "" {
			if _, err := fmt.Fprintf(w, "- **Action:** %s\n", c.SuggestedAction); err != nil {
				return err
			}
		}
		if c.DrillInCmd != "" {
			if _, err := fmt.Fprintf(w, "- **Drill in:** `%s`\n", c.DrillInCmd); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeReviewerLoop(w io.Writer, v LoopView) error {
	if _, err := fmt.Fprintln(w, "\n## Reviewer loop"); err != nil {
		return err
	}
	for _, r := range v.Reviewers {
		if err := writeReviewer(w, r); err != nil {
			return err
		}
	}
	return writeLoopDone(w, v)
}

func writeReviewer(w io.Writer, r ReviewerView) error {
	if _, err := fmt.Fprintf(w, "\n### %s\n", formatIdentity(r.Identity)); err != nil {
		return err
	}
	header := []string{
		fmt.Sprintf("- **Phase:** %s", r.Phase),
		fmt.Sprintf("- **Rally:** %d/%d", r.RallyCount, r.MaxRallies),
		fmt.Sprintf("- **Goal:** `%s` (met: %v)", r.Goal, r.GoalMet),
	}
	for _, line := range header {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	if err := writeReviewerComments(w, r); err != nil {
		return err
	}
	if err := writeReviewBodyPointer(w, r); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "\n**Next action:** %s\n", nextAction(r))
	return err
}

// writeReviewBodyPointer surfaces the presence of a review-level body (e.g.
// CodeRabbit "outside diff range" findings that are not attached to any inline
// thread). The body is untrusted and often mostly boilerplate, so it is never
// inlined here: concise mode (check) points at the view command, full mode
// (view) emits a drill-in command that reads the body on demand.
func writeReviewBodyPointer(w io.Writer, r ReviewerView) error {
	if !r.ReviewBodyPresent {
		return nil
	}
	on := ""
	if r.LatestReviewCommitOID != "" {
		on = fmt.Sprintf(" on `%s`", shortOID(r.LatestReviewCommitOID))
	}
	if r.ReviewBodyDrillInCmd != "" {
		_, err := fmt.Fprintf(w, "\n**Review body** (%s review%s) — read it:\n```sh\n%s\n```\n",
			r.LatestReviewState, on, r.ReviewBodyDrillInCmd)
		return err
	}
	_, err := fmt.Fprintf(w,
		"\nℹ️ Latest %s review%s has a body (may contain findings not tied to a thread) — "+
			"read it with `%s view --condition reviewers`.\n",
		r.LatestReviewState, on, programName)
	return err
}

// shortOID truncates a commit OID to its first 7 characters for display.
func shortOID(oid string) string {
	const n = 7
	if len(oid) <= n {
		return oid
	}
	return oid[:n]
}

// writeReviewerComments renders comment bodies in full mode, or the unresolved
// count plus a drill-in command in concise mode.
func writeReviewerComments(w io.Writer, r ReviewerView) error {
	if len(r.UnresolvedComments) > 0 {
		return writeFullComments(w, r.UnresolvedComments)
	}
	if r.UnresolvedCount > 0 {
		return writeConciseComments(w, r.UnresolvedCount, r.DrillInCmd)
	}
	return nil
}

// writeFullComments renders each unresolved comment body as an inert fenced
// code block. Reviewer-authored text is untrusted input: this output is fed to
// an AI agent, so rendering a body as raw Markdown would let a comment inject
// headings or imperative instructions the agent might follow. A code fence
// neutralizes the body — the Author/URL metadata stays as plain Markdown.
func writeFullComments(w io.Writer, comments []CommentView) error {
	if _, err := fmt.Fprintf(w, "\n**Unresolved comments (%d):**\n", len(comments)); err != nil {
		return err
	}
	for _, c := range comments {
		meta := c.Author
		if c.URL != "" {
			meta = fmt.Sprintf("%s — <%s>", meta, c.URL)
		}
		fence := codeFenceFor(c.Body)
		if _, err := fmt.Fprintf(w, "\n- %s\n\n%s\n%s\n%s\n", meta, fence, c.Body, fence); err != nil {
			return err
		}
	}
	return nil
}

// codeFenceFor returns a backtick fence longer than the longest run of
// backticks in body, so an embedded fence in the body cannot close ours early.
// The minimum length is 3.
func codeFenceFor(body string) string {
	longest, run := 0, 0
	for _, r := range body {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	const minFenceLen = 3 // a Markdown code fence is at least three backticks
	return strings.Repeat("`", max(longest+1, minFenceLen))
}

func writeConciseComments(w io.Writer, count int, drillIn string) error {
	if _, err := fmt.Fprintf(w, "- **Unresolved:** %d thread(s)\n", count); err != nil {
		return err
	}
	if drillIn == "" {
		return nil
	}
	_, err := fmt.Fprintf(w, "\nRead the unresolved comments:\n```sh\n%s\n```\n", drillIn)
	return err
}

func nextAction(r ReviewerView) string {
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
		// A changes-requested review is the formal blocker: it supersedes the
		// generic active guidance because it must be cleared by a re-review.
		if r.ChangesRequested {
			return changesRequestedAction(r)
		}

		// Unresolved conversations must be addressed before any re-request: a
		// re-request does not advance the goal while the reviewer's existing
		// comments are still open.
		if n := r.unresolvedCount(); n > 0 {
			return fmt.Sprintf(
				"Resolve the %d unresolved conversation(s) first (read them above): address each "+
					"with a fix or reply and mark it resolved, then push. Re-request only once no "+
					"unresolved conversations remain.",
				n,
			)
		}

		if r.CanRerequest {
			return fmt.Sprintf(
				"(re)request review: run `%s request --reviewer %s`. Then poll in a BACKGROUND "+
					"shell so the foreground is not blocked — run `sleep 60 && %s check` as a "+
					"background job (do not run it in the foreground) — and re-check when it returns.",
				programName, formatIdentity(r.Identity), programName,
			)
		}

		return fmt.Sprintf(
			"Re-request blocked: %s. Push a new commit if changes are needed, or wait if a "+
				"request is already pending.",
			r.BlockReason,
		)
	}

	// unreachable: all Phase values handled above
	return ""
}

// changesRequestedAction returns the next-action for a reviewer whose latest
// review requests changes. When a re-request can advance the loop the agent
// should address and re-request; otherwise it must escalate rather than spin,
// because the tool cannot make the PR mergeable on its own.
func changesRequestedAction(r ReviewerView) string {
	id := formatIdentity(r.Identity)
	if r.CanRerequest {
		return fmt.Sprintf(
			"%s requested changes. Read the review body, address the feedback (and resolve any open "+
				"threads), push a commit, then re-request: `%s request --reviewer %s`. Then poll in a "+
				"BACKGROUND shell — run `sleep 60 && %s check` as a background job (do not run it in the "+
				"foreground).",
			id, programName, id, programName,
		)
	}
	return fmt.Sprintf(
		"%s requested changes and a re-request is blocked: %s. If there are changes you can make, push "+
			"them. If you cannot address the request, STOP and escalate to a human — this PR cannot be "+
			"made mergeable automatically. If a request is already pending, wait for the reviewer to respond.",
		id, r.BlockReason,
	)
}

// unresolvedCount returns the number of unresolved threads for the reviewer,
// covering both concise mode (UnresolvedCount) and full mode (UnresolvedComments).
func (r ReviewerView) unresolvedCount() int {
	if r.UnresolvedCount > 0 {
		return r.UnresolvedCount
	}
	return len(r.UnresolvedComments)
}

func writeLoopDone(w io.Writer, v LoopView) error {
	if !v.Done {
		return nil
	}

	var exhausted []string
	for _, r := range v.Reviewers {
		if r.Phase == reviewer.PhaseExhausted {
			exhausted = append(exhausted, fmt.Sprintf("`%s`", formatIdentity(r.Identity)))
		}
	}

	if len(exhausted) > 0 {
		_, err := fmt.Fprintf(
			w,
			"\n**Loop complete.** Warning: these reviewers exhausted their rallies without meeting the goal: %s.\n",
			strings.Join(exhausted, ", "),
		)
		return err
	}

	_, err := fmt.Fprintln(w, "\n**Loop complete.** All reviewers reached their goal.")
	return err
}

// formatIdentity returns the canonical "type:name" string for a reviewer identity.
func formatIdentity(id reviewer.Identity) string {
	if id.Name == "" {
		return string(id.Type)
	}

	return fmt.Sprintf("%s:%s", id.Type, id.Name)
}
