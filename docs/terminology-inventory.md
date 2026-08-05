---
doc_goal: Inventory Ward vocabulary observed across CLI, docs, code state names, issue/PR samples, audit language, and agent instructions.
---
# terminology inventory

This inventory describes observed usage. It does not declare every old spelling
preferred.

## Sampled Surfaces

- CLI help for `ward`, `ward exec`, `ward audit`, `ward agent`, and
  `ward container`.
- `.ward/ward.yaml` and `~/.ward/config.yaml`.
- README, FEATURES, architecture, agent, lifecycle, workflow, container,
  reservation, reaper, director, broker, audit, and release docs.
- code workflow comment variants, live queue states, launch phases, list statuses,
  dispatch artifacts, rescue, and salvage paths.
- recent Forgejo issues and PRs covering release runs, broker dispatch, actor
  admission, status summaries, terminal outcomes, and recovery.
- `AGENTS.md` and `.agents/skills/`.

## Product And Authority

- product: `Ward`, `ward`, `warded`, `cli-guard`, `aosguard`, `specgen`.
- authority: tracker thread, forge, git remote, broker, harness credential,
  container env, repo config, workflow label.
- evidence: audit row, audit trail, logs, transcript, status line, issue
  comment, PR status, dispatch artifact.

## Work And Queue

- incoming work: work, issue, ref, issue URL, freeform task, backlog.
- queue objects: issue, pull request, reservation, workflow outcome, live queue snapshot.
- queue states: running, stale, needs redispatch, submitted, merge-ready,
  recover, blocked, failed, and done-but-open.
- pressure: backpressure, open-PR pressure, repo engineer cap, global pool
  ceiling, capacity, stale hold.

## Dispatch And Launch

- dispatch: dispatch request, dispatch broker, request ID, broker accepted,
  broker Ward launch started, dispatch artifact, dispatch-health.
- launch: launch worker, launch path, launch intent, reservation,
  reservation-held, reservation-released, pre-flight, preflight, smoke test,
  launch-adjacent probes, reservation re-check.
- launch phases: broker accepted / queued, pre-flight running, container
  starting, container running, failed before container start.
- launch statuses: partial-launch, cleanup-needed, failed-before-start.

## Roles And Execution

- roles: engineer, director, qa, reviewer.
- harnesses: claude, codex, goose, opencode.
- actor words: agent, worker, role, harness, model, CLI, reviewer, operator.
- execution: run, container, workspace, substrate, context bundle, read-only
  surface, credential broker, bypassPermissions.

## Cleanup And Delivery

- cleanup: stop, reap, cleanup, drain logs, rescue, recover, salvage.
- workflow modes: merge-remote-main, pull-request, pull-request-and-merge,
  remote-branch-only.
- outcomes: landed, submitted, merge-ready, done, blocked, failed, terminal
  outcome, parked outcome.
- release: main, promote, release branch, Forgejo release, install channel,
  GitHub mirror, tag, checksum, draft asset.
