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
2. Look at the exit code and the `status:` line:
   - exit `0` / `status: satisfied` → **stop**. The agent has done everything it
     can; only human-gated advisories (if any) remain.
   - exit `1` / `status: blocked` → there are blockers to resolve. Continue.
   - exit `2` → a usage/config/API error. Read the message and fix the invocation
     (do not loop on this).
3. Resolve each **blocker** by its kind, then go back to step 1:
   - `conflict` → merge or rebase the base branch, resolve conflicts, commit, push.
   - `behind-base` → the repo enforces up-to-date branches. Rebase onto the base
     and push (`git fetch && git rebase origin/<base> && git push --force-with-lease`).
   - `check-failing` → run `mergeable-please view --condition checks` to see which
     required checks failed, investigate the root cause, fix it, push.
   - `check-pending` → required checks are still running. Wait, then re-run check.
   - `merge-eligibility-pending` → GitHub is still computing the merge state. Wait
     15–30 seconds and re-run check.
   - reviewer-loop blockers → follow each reviewer's **Next action** verbatim.
     In general: resolve any unresolved conversations first, then push, then
     (re)request with `mergeable-please request --reviewer <type:name>`.
     Re-requests are blocked until the head advances, so always push your fix
     first. After (re)requesting, the reviewer needs time to respond — poll in a
     **background** shell (e.g. run `sleep 60 && mergeable-please check` as a
     background job, never in the foreground) so you are not blocked.
   - **changes-requested** → a reviewer formally requested changes. Read the
     review body and any threads, address the feedback, push, then re-request.
     If you genuinely cannot address the request (e.g. it asks for a change you
     should not make) and no new commit is possible, **stop and escalate to the
     human** — the tool will keep reporting blocked, which is correct, and you
     must not loop. The reviewer's Next action says exactly this when it applies.
   - **review body** → a reviewer may leave findings in the review body that are
     not attached to any inline thread (e.g. CodeRabbit "outside diff range"
     comments). When `check` notes a review body, read it with
     `mergeable-please view --condition reviewers`, which prints a drill-in
     command for the body.
4. **Advisories** (e.g. `approval-required`, `residual-ruleset`) are NOT blockers
   and never prevent `satisfied`. They require a human (approval) or out-of-scope
   action. Report them to the user as remaining follow-ups; do not try to satisfy
   them yourself. For `residual-ruleset`, `mergeable-please view --condition rules`
   shows the configured ruleset for context.

## Notes

- Prefer the exit code as the stop signal; the `status:` line is the human/agent
  mirror of it.
- Reviewer quality loops are opt-in via `.mergeable-please.yml` at the repo root.
  With no config file, `check` evaluates conflicts and ruleset/CI only.
- After pushing a fix, give CI a moment before re-running `check`.
