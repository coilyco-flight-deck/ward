# ward agent director

`ward agent director` (public face `warded director`) is the **autonomous backlog
supervisor** role of the startup roster (ward#347, was `backlog`): it drives a repo's
headless lane to drain with no human in the inner loop, dispatching engineers and
reconciling their outcomes, reusing ward's own dispatch internals rather than shelling
out. It is the first dispatch-safe slice of ward#324 and subsumes ward#310. It ports the
deterministic backbone of agentic-os's `backlog-loop.py` into ward; the unblock
conversation and human-in-the-loop judgment are deliberately out of this slice.

## The cycle

One run loops until the headless lane drains or a limit is hit:

1. **Refresh** each repo's ledger from the live backlog, ranking open issues into lanes
   by tier (`P0`-`P4`) and mode (`headless`/`interactive`/`consult`) labels.
   `ward exec goose-triage` writes those labels; this loop only reads them.
2. **Dispatch** queued headless-lane issues up to `--max-parallel`, via ward's native
   engineer carry (`runAgentWork`/`launchAgentContainer` in-process, the same path a
   bare-ref `ward agent engineer` takes, audited as `agent.<mode>.engineer`).
3. **Poll** the dispatched containers; on exit, read the `WARD-OUTCOME` comment and
   classify the run `done` / `blocked` / `failed`.
4. **Reconcile** by parking blocked/failed and pulling the next queued issue.
5. **Repeat** on `--poll-interval` until nothing is queued or in flight (or
   `--max-cycles` hits), then print a summary.

Only the **headless** lane is auto-dispatched; interactive/consult issues are surfaced
in the status print, never launched.

## The WARD-OUTCOME marker (ward#310)

A detached engineer carry ends natively by leading its closing retrospective with one line:

```
WARD-OUTCOME: done - <what landed>
WARD-OUTCOME: blocked - <the decision/fact needed from a human>
WARD-OUTCOME: failed - <why>
```

This is baked into the detached seed, so **every** engineer carry emits it. The loop
reads only that line and never injects a protocol comment. A container that exits with
no marker is parked `failed` (`exited-no-outcome`) so the loop drains rather than
spinning - read its log before retrying.

## Scope, ledger, trust

`--repo a/b,c/d` (comma-separated, de-duped, order-preserving) spans many repos in one
run; the default is the cwd git origin. State lives in a durable per-repo YAML ledger
under `~/.ward/backlog/<owner-name>.yaml`, written atomically each transition, so a
killed loop resumes from disk and a dispatched issue is never re-dispatched. Dispatch is
refused unless every scope repo is owned by a trusted org (`ownerAllowed`, mirroring the
engineer carry); the verb audits one row per run.

## Flags

- `--repo a/b,c/d` - scope (default: cwd git origin).
- `--max-parallel N` (2) - in-flight container cap.
- `--driver` (claude) - the harness for dispatched runs.
- `--triage` - run `ward exec goose-triage` across the scope before the first refresh.
- `--limit` (50) - open issues read per repo per refresh.
- `--poll-interval` (30s), `--max-cycles` (0 = until drained), `--dry-run`.

## Out of scope (this slice)

Named scope-as-ward-type (ward#324), the auto-unblock conversation, the full
automation-mode/dispatch-ceiling axis beyond `--max-parallel`, retiring
`backlog-loop.py`, and exposing the loop's steps as CLI sub-verbs.

## See also

- [docs/agent.md](agent.md) - the `ward agent` roster and the `warded` face.
- [docs/agent-subcommands.md](agent-subcommands.md) - the roles, including this one.
- [docs/agent-engineer.md](agent-engineer.md) - the carry the director dispatches.
