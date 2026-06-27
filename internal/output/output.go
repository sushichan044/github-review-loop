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
//     bodies; they are rendered inline (see [Render]).
//   - Concise mode (check): set UnresolvedCount; leave UnresolvedComments empty. The
//     check task list shows the count per reviewer (see [RenderCheckResult]).
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

// writeTarget writes a "target: <s>" line identifying what was inspected (for
// the GitHub backend, "owner/repo#n <url>"). It writes nothing when s is empty,
// so the renderers stay backend-agnostic: the caller builds the string.
func writeTarget(w io.Writer, target string) error {
	if target == "" {
		return nil
	}
	_, err := fmt.Fprintf(w, "target: %s\n", target)
	return err
}

// taskItem is one line of the check task list: a checkbox state and a label.
type taskItem struct {
	done  bool
	label string
}

// nextStepT is the single highest-priority action for a blocked check.
type nextStepT struct {
	action  string
	command string
}

// RenderCheckResult writes a [core.CheckResult] as a lean task list. The first
// line is the machine-readable "status: satisfied|blocked …" header (the loop
// stop signal mirrors the exit code); the body is one checkbox per merge
// condition and reviewer, trailing "~" lines for human-only advisories, and a
// single "Next →" step for the highest-priority outstanding item. Depth (failing
// check names, comment bodies, ruleset rules) lives in `view`, not here.
func RenderCheckResult(w io.Writer, r core.CheckResult, loopView *LoopView, target string) error {
	status := "satisfied"
	if !r.Satisfied {
		status = "blocked"
	}

	head := "status: " + status
	if target != "" {
		head += " · " + target
	}
	lines := []string{head, ""}

	for _, it := range checkTaskItems(r, loopView) {
		box := " "
		if it.done {
			box = "x"
		}
		lines = append(lines, fmt.Sprintf("- [%s] %s", box, it.label))
	}

	for _, a := range advisoryLines(r, loopView) {
		lines = append(lines, "~ "+a)
	}

	if step := nextStep(r, loopView); step != nil {
		lines = append(lines, "", "Next → "+step.action)
		if step.command != "" {
			lines = append(lines, "       "+step.command)
		}
	}

	for _, ln := range lines {
		if _, err := fmt.Fprintln(w, ln); err != nil {
			return err
		}
	}
	return nil
}

// checkTaskItems returns the fixed-order task list: the merge dimensions
// (conflicts, base, required checks, and merge-eligibility only while pending)
// followed by one item per reviewer. A dimension is done when no blocker of its
// kind is present.
func checkTaskItems(r core.CheckResult, lv *LoopView) []taskItem {
	items := []taskItem{
		{done: !hasBlockerKind(r.Blockers, core.ConditionConflict), label: "conflicts"},
		{done: !hasBlockerKind(r.Blockers, core.ConditionBehindBase), label: "base up-to-date"},
		requiredChecksItem(r.Blockers),
	}
	if hasBlockerKind(r.Blockers, core.ConditionMergeEligibilityPending) {
		items = append(items, taskItem{label: "merge eligibility — still computing"})
	}
	if lv != nil {
		for _, rv := range lv.Reviewers {
			items = append(items, reviewerTaskItem(rv))
		}
	}
	return items
}

// requiredChecksItem collapses the failing/pending required-check blockers into a
// single line, naming the checks when the attribution provided them.
func requiredChecksItem(blockers []core.Condition) taskItem {
	var parts []string
	if hasBlockerKind(blockers, core.ConditionCheckFailing) {
		parts = append(parts, labelWithDetail(blockerDetail(blockers, core.ConditionCheckFailing), "failing"))
	}
	if hasBlockerKind(blockers, core.ConditionCheckPending) {
		parts = append(parts, labelWithDetail(blockerDetail(blockers, core.ConditionCheckPending), "pending"))
	}
	if len(parts) == 0 {
		return taskItem{done: true, label: "required checks"}
	}
	return taskItem{label: "required checks — " + strings.Join(parts, ", ")}
}

// labelWithDetail renders "<detail> <suffix>" when detail is present, else suffix.
func labelWithDetail(detail, suffix string) string {
	if detail == "" {
		return suffix
	}
	return detail + " " + suffix
}

// reviewerTaskItem maps one reviewer's state to a task line. Terminal phases
// (goal-met, exhausted) are done; an active reviewer is outstanding and labeled
// by the reason it is still blocking.
func reviewerTaskItem(rv ReviewerView) taskItem {
	id := formatIdentity(rv.Identity)
	switch rv.Phase {
	case reviewer.PhaseGoalMet:
		return taskItem{done: true, label: id + " — goal met"}

	case reviewer.PhaseExhausted:
		return taskItem{done: true, label: fmt.Sprintf("%s — exhausted (%d/%d) ⚠", id, rv.RallyCount, rv.MaxRallies)}

	case reviewer.PhaseActive:
		rally := fmt.Sprintf("(%d/%d)", rv.RallyCount, rv.MaxRallies)
		switch {
		case rv.ChangesRequested:
			return taskItem{label: fmt.Sprintf("%s — changes requested %s", id, rally)}
		case rv.unresolvedCount() > 0:
			return taskItem{label: fmt.Sprintf("%s — %d unresolved %s", id, rv.unresolvedCount(), rally)}
		case rv.CanRerequest:
			return taskItem{label: fmt.Sprintf("%s — awaiting review %s", id, rally)}
		default:
			return taskItem{label: fmt.Sprintf("%s — awaiting response %s", id, rally)}
		}
	}

	// unreachable: all Phase values handled above
	return taskItem{label: id}
}

// advisoryLines returns the trailing "~" notes: human-only advisories, an
// exhausted-reviewer warning, and a single review-notes pointer when any
// reviewer left a review body.
func advisoryLines(r core.CheckResult, lv *LoopView) []string {
	var lines []string
	for _, a := range r.Advisories {
		lines = append(lines, advisoryLabel(a))
	}
	if lv == nil {
		return lines
	}
	for _, rv := range lv.Reviewers {
		if rv.Phase == reviewer.PhaseExhausted {
			lines = append(lines, fmt.Sprintf(
				"%s exhausted %d/%d rallies without meeting goal — stop the loop or raise max-rallies",
				formatIdentity(rv.Identity), rv.RallyCount, rv.MaxRallies))
		}
	}
	for _, rv := range lv.Reviewers {
		if rv.ReviewBodyPresent {
			lines = append(lines, "review notes present — "+programName+" view --condition reviewers")
			break
		}
	}
	return lines
}

// advisoryLabel renders a condition as a terse one-line advisory note. Only the
// known advisory kinds get a custom label; any other kind falls back to Title.
func advisoryLabel(c core.Condition) string {
	//nolint:exhaustive // partial by design: unlisted kinds use the Title fallback.
	switch c.Kind {
	case core.ConditionApprovalRequired:
		return "approval required (human)"
	case core.ConditionChangesRequested:
		return "changes requested via reviewDecision (human)"
	case core.ConditionResidualRuleset:
		return "ruleset block — " + programName + " view --condition rules (human)"
	case core.ConditionCheckTruncated:
		return "check list truncated at 100 — " + programName + " view --condition checks"
	default:
		return c.Title
	}
}

// nextStep returns the single highest-priority action for a blocked check, or
// nil when nothing is outstanding. Dimensions are tried in fix-first order, then
// reviewers; only the top item is expanded so the agent has one clear move.
func nextStep(r core.CheckResult, lv *LoopView) *nextStepT {
	dimSteps := []struct {
		kind    core.ConditionKind
		action  string
		command string
	}{
		{core.ConditionConflict, "resolve merge conflicts (merge or rebase the base branch), commit, push", ""},
		{core.ConditionBehindBase, "rebase onto the base branch and push", ""},
		{
			core.ConditionCheckFailing,
			"fix the failing required checks, then push",
			programName + " view --condition checks",
		},
		{core.ConditionCheckPending, "wait for required checks to finish, then re-run check", ""},
		{
			core.ConditionMergeEligibilityPending,
			"wait ~30s for GitHub to compute the merge state, then re-run check",
			"",
		},
	}
	for _, s := range dimSteps {
		if hasBlockerKind(r.Blockers, s.kind) {
			return &nextStepT{action: s.action, command: s.command}
		}
	}

	if lv == nil {
		return nil
	}
	for _, rv := range lv.Reviewers {
		if rv.Phase != reviewer.PhaseActive {
			continue
		}
		return reviewerNextStep(rv)
	}
	return nil
}

// reviewerNextStep returns the action for an active reviewer that is the top
// outstanding item.
func reviewerNextStep(rv ReviewerView) *nextStepT {
	id := formatIdentity(rv.Identity)
	switch {
	case rv.ChangesRequested:
		return &nextStepT{
			action: fmt.Sprintf(
				"address %s's requested changes (read the body), push, then re-request — or escalate if you cannot",
				id,
			),
			command: programName + " view --condition reviewers",
		}
	case rv.unresolvedCount() > 0:
		return &nextStepT{
			action:  fmt.Sprintf("resolve %s's %d unresolved thread(s), then push", id, rv.unresolvedCount()),
			command: programName + " view --condition reviewers",
		}
	case rv.CanRerequest:
		return &nextStepT{
			action: fmt.Sprintf(
				"re-request %s, then poll in a background shell (sleep 60 && %s check)",
				id,
				programName,
			),
			command: programName + " request --reviewer " + id,
		}
	default:
		return &nextStepT{action: fmt.Sprintf("%s: %s", id, rv.BlockReason)}
	}
}

// hasBlockerKind reports whether any blocker has the given kind.
func hasBlockerKind(blockers []core.Condition, kind core.ConditionKind) bool {
	for _, b := range blockers {
		if b.Kind == kind {
			return true
		}
	}
	return false
}

// blockerDetail returns the Detail of the first blocker with the given kind.
func blockerDetail(blockers []core.Condition, kind core.ConditionKind) string {
	for _, b := range blockers {
		if b.Kind == kind {
			return b.Detail
		}
	}
	return ""
}

// RenderDimensionView renders a filtered set of conditions for a single dimension
// (e.g. view --condition checks). It emits NO global "status:" verdict — only the
// matching conditions — because a partial view that omits the reviewer loop must
// not imply an authoritative pass/fail.
func RenderDimensionView(w io.Writer, blockers, advisories []core.Condition, target string) error {
	if err := writeTarget(w, target); err != nil {
		return err
	}
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
// including comment bodies. target identifies the inspected PR (may be empty).
func Render(w io.Writer, v LoopView, target string) error {
	if err := writeTarget(w, target); err != nil {
		return err
	}
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
