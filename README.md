# mergeable-please

Drive a GitHub pull request toward **mergeable** — automatically.

`mergeable-please` is a stateless, single-shot CLI that an AI coding agent runs
repeatedly to push a PR to the green state it can reach on its own. Each run
diagnoses every merge condition, reports the blockers an agent can fix and the
advisories only a human can, and exits `0` once nothing actionable remains. The
agent (via the bundled skill) drives the loop by re-running after each fix.

It deliberately does **not** try to satisfy human-gated conditions (such as a
required human approval): those are surfaced as advisories and never block the
"done" determination.

## What it checks

| Dimension | Kind | Notes |
|---|---|---|
| Merge conflicts with the base branch | blocker | From the PR `mergeStateStatus` on GitHub (no local git needed). |
| Behind the base branch | blocker | Only when the repository ruleset enforces up-to-date branches. |
| Required CI / status checks | blocker | Failing → fix; pending → wait. Only checks that are *required* for the PR. |
| Reviewer quality loop | blocker (opt-in) | Loops AI review tools (Copilot, CodeRabbit, …) to their goal before merge. |
| Human approval, signed commits, deployment gates, … | advisory | Reported for awareness; never block `satisfied`. |

`done` (`status: satisfied`, exit `0`) = no blockers remain **and** every
configured reviewer loop has reached its goal or exhausted its rallies.

## Usage

```sh
mergeable-please check            # diagnose the current branch's PR
mergeable-please check 42         # diagnose PR #42 in the current repo
mergeable-please check <pr-url>   # diagnose by GitHub URL

mergeable-please view --condition checks   # drill into required-check detail
mergeable-please view --condition rules    # show the base-branch ruleset

mergeable-please request --reviewer github-copilot   # guarded reviewer re-request
mergeable-please init                                 # write a config template
```

Exit codes: `0` mergeable · `1` blocked · `2` usage/config/API error.

Authentication uses the same source as the `gh` CLI (`gh auth token`, or
`GH_TOKEN`); no separate setup is required.

## Configuration

Configuration is optional. With no file, `check` evaluates conflicts and
ruleset/CI using sensible defaults. To enable a reviewer quality loop, commit a
`.mergeable-please.yml` at the repository root:

```yaml
git:
  remote: origin
  conflicts-resolved: true
github:
  rulesets: true
  reviewers:
    - type: github-copilot
      goal: { all-conversations-resolved: true }
      max-rallies: 5
```

Run `mergeable-please init` to generate a commented template.

## Use with an AI agent

The binary embeds an agent skill. Install it into your agent's skills directory:

```sh
mergeable-please skills install                # ~/.agents/skills (user scope)
mergeable-please skills install --scope repo   # <repo-root>/.agents/skills
```

Then ask your agent something like *"use mergeable-please to get this PR
mergeable"* and it will run the check/fix loop until the PR is ready.

## Development

```sh
mise run test        # tests with coverage
mise run lint        # golangci-lint
mise run lint:fix    # auto-fix
mise run fmt         # format
```
