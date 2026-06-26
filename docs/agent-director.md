# ward agent director

`ward agent director` (public face `warded director`) is the **autonomous backlog
supervisor** role (ward#347, was `backlog`): it drives a repo's headless lane to drain via
ward's own internals. ward#346 ported `backlog-loop.py`; ward#351 made it an **LLM-in-the-
loop heartbeat** that surfaces an interactive session on drain.

## The init gate (ward#361)

At startup, **before the first drain tick**, director asks once - "drain the headless
backlog now?" **yes** (or a bare Enter) begins the autonomous drain; **no** surfaces an
interactive session first, and a drain begins once headless work is queued. A deliberate
opt-in, not auto-started (ward#350); asked **once at init**, never per tick or on resume;
no terminal drains, and `--dry-run`/`--print` skip it.

## The heartbeat (ward#351)

`director` is **attached/interactive only** - no `--detach` (runaway-dispatch risk). Each
tick:

1. **Poll + reconcile** in-flight engineers: on exit read each `WARD-OUTCOME` and classify
   the run `done` / `blocked` / `failed`.
2. **Refresh** each ledger from the live backlog, ranking issues into lanes by tier
   (`P0`-`P4`) and mode (`headless`/`interactive`/`consult`) labels.
3. **Decide** via a host one-shot handed the ranked candidates, budget, in-flight set, and
   outcomes; it answers `DISPATCH: <numbers>`/`none`, can only **narrow or hold**, and
   **fails open to rank** (#346) on an unclear read.
4. **Dispatch** the chosen set via the native engineer carry (`agent.<mode>.engineer`).
5. **Sleep** `--poll-interval`, **no LLM held open**.

Only the **headless** lane auto-dispatches; interactive/consult are surfaced.

## Drain → surface (ward#351)

When the lane drains - **nothing queued or in flight** - director does not exit: it surfaces
a **read-only architect session** on the lead repo, blocking until the human exits; the
heartbeat **resumes** if the queue refilled, else stops (ward#350).

## The WARD-OUTCOME marker (ward#310)

A detached engineer carry leads its retrospective with `WARD-OUTCOME: done` (or
`blocked`/`failed`). The loop reads only that line; a no-marker exit is parked `failed`.

## Scope, ledger, trust

`--repo a/b,c/d` spans many repos (de-duped); default is the cwd git origin. `--org <org>`
(repeatable; ward#370) expands to every repo that org owns, unioned with `--repo`; an empty
expansion errors; `--dry-run`/`--print` show the list. State lives in a per-repo
YAML ledger under `~/.ward/backlog/`, so a killed loop resumes and an issue is never
re-dispatched. Dispatch is refused unless every scope repo (expansions included) is a
trusted owner; one audit row per run.

## Flags

- `--repo a/b,c/d` scope; `--org <org>` repo-list scope (ward#370); `--max-parallel N` (2);
  `--triage`; `--limit` (50); `--poll-interval` (30s); `--max-cycles` (0=until drained); `--dry-run`.
- `--driver` (claude) drives director's OWN session (decision one-shot + drain surface);
  `--engineer-driver` overrides the dispatched-engineer harness, else it inherits `--driver`
  (ward#355, two-level).
- Container/harness parity (ward#355): `--image`/`--tag`, `--ward-source`/`--ward-version`,
  `--aws`, `--host-net`/`--ts-sidecar`, `--no-pull`, `--with-repo`, `--print`, `--force`. The
  dispatch-configuring subset propagates into each engineer; the full set configures the drain
  surface. `--print` shows that plan, launches nothing. `--branch`/`--no-preflight` and
  `--watch`/`--detach` are absent (ward#350).

## Reservation conflicts defer (ward#352)

A dispatch onto a held reservation (another run, 2h TTL) is **deferred**, not failed: left
eligible, retried later, off the failure tally. Only a real launch error parks `failed`.
`--force` makes engineers reclaim a stale/foreign hold.

## See also

- [docs/agent.md](agent.md) - the `ward agent` roster + `warded` face.
- [docs/agent-subcommands.md](agent-subcommands.md) - the roles, this one included.
- [docs/agent-engineer.md](agent-engineer.md) - the carry it dispatches.
