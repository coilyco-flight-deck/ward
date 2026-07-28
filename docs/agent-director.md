---
doc_goal: Give the director surface one durable description so the read-only lane and its merge-ready follow-through do not live in scattered issue pages.
---
# ward agent director

The director surface is the read-only control plane for runs.

- It can inspect the fleet, read logs, and stop a run.
- It can keep a backlog moving without writing implementation code.
- It can also run against one exact issue ref or Forgejo issue URL without
  widening into the repo backlog.
* `~/.ward/config.yaml` provides implicit scope; `--repo` / `--org` override it.
  If an attached no-scope director has no `director.default-scope`, Ward prompts
  for a repo/org default and saves it. Headless launches still fail closed. The
  director needs no git cwd; AOSguard config cannot alter native policy.
- By default it prints status from the stored ledger, then opens the attached
  read-only surface without enumerating the live issue backlog. Add `--burndown`
  to run the autonomous dispatch heartbeat, or `--triage` to opt into startup issue inventory.
- The bundled burndown default comes from `.ward/defaults.kdl`, which keeps the interactive lane on the bundled smart-defaults file without hardcoding a number in docs.
- When issue-scoped under `--burndown`, each heartbeat refresh stays pinned to
  that exact issue instead of rehydrating the repo backlog.
- It is the surface that hosts the merge-ready workflow for PR landings.
- It distinguishes a fresh reservation hold from a stale one so dead runs do
  not block burndown forever.

## Typical uses

- check whether an engineer is still alive.
- read the last logs before deciding whether to re-dispatch.
- inspect the queue/status view for stale reservations, redispatch candidates, PR-open handoffs, closed-unmerged PR recovery, and stale-open done issues.
- stop a run that is definitely on the wrong ref.
- target one issue by `owner/repo#N` or full Forgejo issue URL when the run
  should stay scoped to a single decision payload.
- launch from any working directory when the repo scope is explicit or
  configured.
- opt into autonomous headless dispatch with `--burndown` or `--drain`.
- sweep the merge-ready branch once CI is green.
- update the oldest merge-ready PR branch when open PR pressure is over cap and
  the branch still conflicts with main.

The director's machine-readable issue comments use `WARD-WORKFLOW:` as the canonical first line. `WARDED_WORKFLOW:` and the older typed `WARD-*` headers remain parser compatibility for old threads, but new PR handoffs start with `WARD-WORKFLOW: <fully-qualified pull request link>`.
Review-gated `pull-request-and-merge` handoffs keep the first line as the PR URL
and carry `director merge authorization: reviewed-and-ready` in the details
block so the merge sweep can still recognize the ready state.

## Starting interactively

There is no separate public `warded surface` command:

```bash
warded director --repo owner/name
warded director # prompts once for repo/org default scope if none is configured
warded director --burndown --repo owner/name # autonomous drain
```

The first form refreshes status and opens the read-only session without
dispatching engineers. `--drain` aliases `--burndown`. During burndown, press
Enter at the sleep offer to open the same session.

The director surface is intentionally narrower than the engineer path. It is
for supervision and landing, not for implementation.

## What it is not

- it is not a shell into the target repo.
- it is not a general-purpose container admin surface.
- it is not a replacement for the issue thread.

## See also

- [agent-ops.md](agent-ops.md) - list, logs, stop, reap.
- [agent-pr-workflow.md](agent-pr-workflow.md) - native merge, CI status, and rerun tools.
- [agent-workflow.md](agent-workflow.md) - PR and merge policy.
- [agent-roles.md](agent-roles.md) - role semantics.
