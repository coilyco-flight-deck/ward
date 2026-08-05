---
doc_goal: Give the attached director surface one durable description, including its live startup snapshot and explicit boundary from harness-owned orchestration.
---
# ward agent director

The director is Ward's attached read-only supervision surface.

- It reads one live queue snapshot at startup and persists no orchestration state.
- It can inspect the fleet, read logs, stop a run, and use role-bound broker actions.
- It accepts a repository scope or one exact open issue ref. Exact issue input renders only that issue.
- It does not poll, rank, triage, choose, dispatch, or redispatch work.
- A harness-native goal owns repetition and judgment. The issue thread remains the durable work and lifecycle record.

`~/.ward/config.yaml` may provide `director.default-scope`. Explicit `--repo`
and `--org` values override it. If an attached no-scope director has no default,
Ward prompts for a repository or organization scope and saves that preference.

## Starting the surface

```bash
warded director --repo owner/name
warded director owner/name#123
warded director --org example
warded director --print --repo owner/name
```

The first three forms print one current tracker-backed snapshot and open the
read-only session. `--print` prints that snapshot plus the resolved container
plan and launches nothing. There is no detached or autonomous director mode.

The surface clone cannot push. Its sibling broker can perform only the typed,
role-bound actions Ward exposes. Read-only clone access is therefore distinct
from having no control-plane authority.

## Queue and status

The queue view is a separate read-only command:

```bash
ward agent director queue --repo owner/name
ward agent director status --repo owner/name
ward agent director queue --repo owner/name --json
```

It reads live open issues, pull requests, and trusted machine comments. It
classifies fresh and stale reservations, redispatch candidates, submitted and
merge-ready pull requests, recovery cases, stale-open done issues, blocked
runs, and failed runs. Each carry includes the next operator action.

`--json` emits schema version 1 with deterministic ordering:

- `schema_version`
- `scope`
- action counts under `summary`
- `items` with repository, number, kind, tier, title, state, next action, and optional note

The command reports state. It does not act on that state.

## Goal-driven loop

Ward does not implement this loop. A harness-native `/goal` repeats these
governed primitives until its own completion or blocked judgment:

1. Read the live work surface with `ward agent director queue --json`.
2. Dispatch one bounded carry with `warded engineer owner/name#N`. Reservations,
   capacity, backpressure, and workflow gates still run at launch.
3. Observe broker requests with `ward agent dispatch list --json`. Inspect one
   with `ward agent dispatch status <request-id> --json`. Read the secret-safe
   run artifact with `ward agent logs <request-id>` when a result needs diagnosis.
4. Use the explicit PR, reap, stop, or recovery primitive named by the queue item,
   then read a fresh queue snapshot before choosing again.

The issue thread and dispatch lifecycle remain durable between observations.
The goal owns waiting, prioritization, progress reporting, and the terminal
decision. It must not infer that Ward is polling in the background.

## See also

- [agent-ops.md](agent-ops.md) - list, logs, stop, and reap.
- [agent-pr-workflow.md](agent-pr-workflow.md) - native merge, CI status, and rerun tools.
