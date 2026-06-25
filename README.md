# github-review-loop

A small CLI that manages the **mechanics** of an AI/human code-review loop on a GitHub pull
request — one reviewer at a time, until each reaches its goal or a safety limit.

It is **not** a reviewer and it does **not** judge whether comments were addressed well. Reading
the feedback, fixing the code, and pushing is still done by you (or your AI agent). This tool only
removes the *bookkeeping* uncertainty of running a review loop:

1. **No premature re-requests** — it refuses to re-request a reviewer that has already reviewed the
   current commit (push first).
2. **Rallies are counted** — reconstructed from the PR's event history, so the loop can't run away.
3. **You always know where you stand** — every reviewer's state plus a concrete next action.

See [`docs/spec.md`](docs/spec.md) for the full design rationale.

## Install

```bash
go install github.com/sushichan044/github-review-loop@latest
```

This produces a `github-review-loop` binary on your `PATH`.

## Authentication

The tool reuses the [GitHub CLI](https://cli.github.com/)'s credentials — there is no separate
token to manage. Make sure you're logged in:

```bash
gh auth login
```

(The `GH_TOKEN` / `GITHUB_TOKEN` environment variables are honored as a fallback, same as `gh`.)

## Configuration

Config lives at `<config-home>/github-review-loop/config.yaml`, where `<config-home>` is
`$XDG_CONFIG_HOME` (default `~/.config` on macOS/Linux) or the OS config dir on Windows.

The top-level key is `loops`. Each entry is a policy scoped to either an `owner` (applies to all of
that owner's repos) or a single `repo`. For a given repository, the matching `owner` policy and
`repo` policy are **merged by reviewer identity** (`type` + `name`): a reviewer defined at `repo`
scope overrides the same reviewer at `owner` scope, repo-only reviewers are added, and owner-only
reviewers are kept.

```yaml
loops:
  - scope: owner
    owner: sushichan044
    reviewers:
      - type: user
        name: sushichan044
        goal: { approved: true }            # done when this user approves
        max-rallies: 5                       # safety limit (default 5 if omitted)

      - type: github-copilot
        goal: { all-conversations-resolved: true }  # done when no unresolved review threads remain
        max-rallies: 5

      - type: github-app
        name: coderabbitai
        goal: { approved: true }
        max-rallies: 5
        trigger: "@coderabbitai review"      # github-app reviewers can be triggered by a comment

  - scope: repo
    owner: sushichan044
    repo: github-review-loop
    reviewers:
      # merged onto the owner policy by identity; this overrides github-copilot's max-rallies
      - type: github-copilot
        goal: { all-conversations-resolved: true }
        max-rallies: 3
```

### Reviewer fields

| Field         | Required                          | Meaning |
|---------------|-----------------------------------|---------|
| `type`        | yes                               | `user`, `github-copilot`, or `github-app` |
| `name`        | for `user` and `github-app`       | the login (for `github-app`, the bot login, e.g. `coderabbitai`) |
| `goal`        | yes                               | exactly one of `approved: true` or `all-conversations-resolved: true` |
| `max-rallies` | no (default `5`)                  | safety limit, independent of the goal |
| `trigger`     | no                                | for `github-app`: a comment string posted to trigger the bot (e.g. `@coderabbitai review`) |

## Usage

```bash
# Show the loop status for the current branch's PR (or pass a PR number / URL)
github-review-loop status
github-review-loop status 42
github-review-loop status https://github.com/owner/repo/pull/42

# Re-request review from every eligible reviewer (the guard skips ineligible ones)
github-review-loop request

# Re-request a single reviewer
github-review-loop request --reviewer github-app:coderabbitai

# Force a specific output format
github-review-loop status --format human
github-review-loop status --format agent
```

### `status`

For each configured reviewer, `status` reports:

- **phase** — `active`, `goal-met`, or `exhausted`
- **rally `N/max`** — how many times the reviewer has been triggered vs. its `max-rallies`
- **goal** and whether it's met
- **unresolved review-thread comments** (bodies) and **new comments since the last rally**
- a **next action** — always printed, telling you what to do next (request, push and re-request,
  or stop)

### `request`

Fires a re-request for the targeted reviewers — but only those the **guard** allows. A reviewer
whose latest review is already on the current head commit is skipped with the reason printed
(push a new commit first). Reviewers in a terminal phase (`goal-met` / `exhausted`) are skipped
too. With no `--reviewer`, all eligible reviewers are requested.

How a reviewer is triggered depends on its type:

- `user` / `github-copilot` → added as a PR reviewer (`gh pr edit --add-reviewer …`)
- `github-app` → added as a reviewer by default, or, if a `trigger` string is configured, that
  string is posted as a PR comment

### `--format`

`--format human|agent` controls output representation only — never behavior. The default is
auto-detected: `agent` when running inside an AI coding agent, otherwise `human`. An explicit flag
always wins. In `agent` format the next-action guidance is more verbose and, when you should wait
for a reviewer, suggests waiting in a background shell and re-running `status`.

## Concepts

- **Rally** — one trigger action for a reviewer (a review-request event, or a trigger comment for
  comment-driven bots). The initial request counts as rally 1. Rallies are reconstructed from the
  PR timeline, so requests made outside this tool (e.g. from the GitHub UI) are counted too.
- **Goal** — `approved` (the reviewer's latest non-pending review is an approval) or
  `all-conversations-resolved` (no unresolved review-conversation threads remain for that
  reviewer; issue comments are not counted).
- **Guard** — re-request is a no-op while the reviewer's latest review is on the current head
  commit, so you can't ask again without pushing changes.
- **Phases** — `active` (still looping) → terminal `goal-met` (goal reached) or `exhausted`
  (`max-rallies` hit without the goal). The loop is complete when every reviewer is terminal.

## Development

```bash
mise run test        # run tests with coverage
mise run lint        # golangci-lint + deadcode + go fix
mise run lint:fix    # auto-fix
mise run fmt         # format
```
