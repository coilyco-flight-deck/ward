# ward agent director

`ward agent director` (public face `warded director`) is the **autonomous backlog
supervisor** role (ward#347, was `backlog`): it drives a repo's headless lane to drain,
dispatching engineers and reconciling outcomes via ward's own internals.
Its deterministic backbone ports agentic-os's `backlog-loop.py` (ward#346, subsuming
ward#310); ward#351 turned the loop into an **LLM-in-the-loop heartbeat** that surfaces
an interactive session on drain instead of exiting.

## The heartbeat (ward#351)

`director` is **attached/interactive only** - there is no `--detach` (a detached director
poses runaway-dispatch risk). Each tick:

1. **Poll + reconcile** in-flight engineers: on exit read each `WARD-OUTCOME` comment and
   classify the run `done` / `blocked` / `failed`.
2. **Refresh** each ledger from the live backlog, ranking issues into lanes by tier
   (`P0`-`P4`) and mode (`headless`/`interactive`/`consult`) labels (`goose-triage`
   writes them; the loop only reads).
3. **Decide** via a host one-shot (`claude -p` / `goose run -t`) handed the ranked queued
   candidates, the free-slot budget, the in-flight set, and recent outcomes; it answers on
   a `DISPATCH: <numbers>` / `DISPATCH: none` line. It can only **narrow or hold** what
   rank offers, and **fails open to rank** (no host one-shot, no binary, a timed-out read,
   or no clear verdict all dispatch the top `avail` picks - the #346 floor).
4. **Dispatch** the chosen set via the native engineer carry (the bare-ref path, audited
   `agent.<mode>.engineer`).
5. **Sleep** `--poll-interval` with **no LLM held open**, so an idle heartbeat is free.

Only the **headless** lane is auto-dispatched; interactive/consult issues are surfaced,
never launched.

## Drain → surface (ward#351)

When the lane drains - **nothing queued and nothing in flight** - the director does not
exit. It reports the disposition (done / parked-blocked / parked-failed) and surfaces a
**read-only architect session** on the scope's lead repo for new direction. That blocks
until the human exits; the heartbeat then **resumes** if the queue refilled, else stops.
With no terminal it cannot surface, so a drain there exits cleanly. Collapsing the
standalone `architect` into this phase is a later slice (ward#350).

## The WARD-OUTCOME marker (ward#310)

A detached engineer carry ends by leading its closing retrospective with one line:

```
WARD-OUTCOME: done - <what landed>
WARD-OUTCOME: blocked - <the decision/fact needed from a human>
WARD-OUTCOME: failed - <why>
```

Baked into the seed, so every carry emits it; the loop reads only that line. A container
that exits with no marker is parked `failed` (`exited-no-outcome`) - read its log.

## Scope, ledger, trust

`--repo a/b,c/d` (comma-separated, de-duped) spans many repos; the default is the cwd git
origin. State lives in a durable per-repo YAML ledger under `~/.ward/backlog/`, written
atomically each transition, so a killed loop resumes and a dispatched issue is never
re-dispatched. Dispatch is refused unless every scope repo is a trusted owner
(`ownerAllowed`); the verb audits one row per run.

## Flags

- `--repo a/b,c/d` - scope (default: cwd git origin); `--max-parallel N` (2) - in-flight cap.
- `--driver` (claude) - harness for dispatched runs and the per-tick decision.
- `--triage` - `ward exec goose-triage` across the scope first; `--limit` (50) per refresh.
- `--poll-interval` (30s) - heartbeat sleep; `--max-cycles` (0=until drained); `--dry-run`.

## Out of scope (this slice)

Named scope-as-ward-type (ward#324), the dispatch-ceiling axis beyond `--max-parallel`,
retiring `backlog-loop.py`, the architect collapse (ward#350), mid-drain steering UX, and
manager-skill refinement.

## See also

- [docs/agent.md](agent.md) - the `ward agent` roster and the `warded` face.
- [docs/agent-subcommands.md](agent-subcommands.md) - the roles, including this one.
- [docs/agent-engineer.md](agent-engineer.md) - the carry the director dispatches.
