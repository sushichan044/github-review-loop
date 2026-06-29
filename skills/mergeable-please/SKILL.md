---
name: mergeable-please
description: Drive a GitHub pull request toward mergeable. Run when asked to "make a PR mergeable", "loop until the PR is ready", "fix the PR until CI/conflicts/reviews pass", or similar. Repeatedly runs `mergeable-please check`, resolves each reported blocker (conflicts, behind-base, failing/pending required CI, reviewer-loop goals), and loops until the tool reports satisfied.
license: MIT
compatibility:
  - claude
  - codex
  - agents
allowed_tools:
  - Bash
  - Read
---

# mergeable-please

Loop a GitHub pull request to merge-readiness. `mergeable-please` is a stateless,
single-shot CLI: it diagnoses every merge condition the agent can act on and
tells you the next action. **You** drive the loop by re-running it after each fix.

## The loop

1. Run `mergeable-please check` (add the PR number or URL if you are not on the
   PR's branch, e.g. `mergeable-please check 42`).
2. `check` prints a task list. The first line is the header
   `status: satisfied|blocked · owner/repo#n <url>`; then one checkbox per merge
   condition and reviewer. Each item may carry indented supplements: a status
   line (e.g. `rally 3/5 · goal approved`) and a `→ action` naming the next move
   and, after `·`, the one `view`/`request` subcommand for depth. Trailing `~`
   lines are human-only notes. Read the exit code and header:
   - exit `0` / `status: satisfied` → **stop**. Every `- [x]` is done; only
     human-gated `~` advisories (if any) remain.
   - exit `1` / `status: blocked` → the `- [ ]` items are outstanding. Work them
     top-down, following each item's indented `→ action`, then go back to step 1.
   - exit `2` → a usage/config/API error. Read the message and fix the invocation
     (do not loop on this).
3. For depth on any item, run the subcommand from its `→ action` (e.g.
   `mergeable-please view --condition checks|conflicts|rules|reviewers`) — `check`
   itself stays terse. Resolve each outstanding `- [ ]` item by its kind, then go
   back to step 1:
   - `conflict` → merge or rebase the base branch, resolve conflicts, commit, push.
   - `behind-base` → the repo enforces up-to-date branches. Rebase onto the base
     and push (`git fetch && git rebase origin/<base> && git push --force-with-lease`).
   - `check-failing` → run `mergeable-please view --condition checks` to see which
     required checks failed, investigate the root cause, fix it, push.
   - `check-pending` → required checks are still running. Wait, then re-run check.
   - `merge-eligibility-pending` → GitHub is still computing the merge state. Wait
     15–30 seconds and re-run check.
   - reviewer `N unresolved` → resolve those conversations first
     (`mergeable-please view --condition reviewers` shows them), push, then
     (re)request with `mergeable-please request --reviewer <type:name>`.
     Re-requests are blocked until the head advances, so always push your fix
     first. After (re)requesting, the reviewer needs time to respond — poll in a
     **background** shell (e.g. run `sleep 60 && mergeable-please check` as a
     background job, never in the foreground) so you are not blocked.
   - reviewer `changes requested` → the reviewer formally requested changes. Read
     the review body and any threads (`view --condition reviewers`), address the
     feedback, push, then re-request. If you genuinely cannot address the request
     (e.g. it asks for a change you should not make) and no new commit is
     possible, **stop and escalate to the human** — the tool will keep reporting
     blocked, which is correct, and you must not loop. The item's `→ action` says
     exactly this when it applies.
   - reviewer `awaiting review` → no outstanding request; (re)request and poll in
     the background as above.
4. The trailing `~` lines are NOT blockers and never prevent `satisfied`:
   - `~ approval required (human)` / `~ ruleset block …` → require a human or
     out-of-scope action. Report them as remaining follow-ups; do not try to
     satisfy them yourself.
   - `~ review notes present …` → a reviewer left a review body that may contain
     findings not tied to any inline thread (e.g. CodeRabbit "outside diff range"
     comments). Read it with `mergeable-please view --condition reviewers`.
   - `~ … exhausted …` → a reviewer used all its rallies without meeting its
     goal; stop the loop for it or raise `max-rallies`.

## Notes

- Prefer the exit code as the stop signal; the `status:` line is the human/agent
  mirror of it.
- Reviewer quality loops are opt-in via `.mergeable-please.yml` at the repo root.
  With no config file, `check` evaluates conflicts and ruleset/CI only.
- After pushing a fix, give CI a moment before re-running `check`.
