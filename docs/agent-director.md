# ward agent director

`ward agent director` (public face `warded director`) is the **autonomous backlog
supervisor** role (ward#347, was `backlog`): it drains a repo's headless lane via ward's own
internals. ward#346 ported `backlog-loop.py`; ward#351 made it a heartbeat.

## Startup triage (ward#397)

Before the init gate, director folds in a **triage pass** (on by default, `--no-triage` skips)
that **writes** the tier + mode labels the heartbeat only ever read - warming the headless
lane. See [director-startup-triage.md](director-startup-triage.md).

## The init gate (ward#361)

At startup, **before the first drain tick**, director asks once - "drain the headless
backlog now?" **yes** (or a bare Enter) begins the autonomous drain; **no** surfaces an
interactive session first. A deliberate opt-in, not auto-started (ward#350); asked **once at
init**, never per tick or on resume; no terminal drains, and `--dry-run`/`--print` skip it.

## The heartbeat (ward#351)

`director` is **attached/interactive only** - no `--detach` (runaway-dispatch risk). Each
tick:

1. **Poll + reconcile** in-flight engineers: on exit read each `WARD-OUTCOME`, classify
   `done`/`blocked`/`failed`.
2. **Refresh** each ledger from the live backlog, ranking issues into lanes by tier
   (`P0`-`P4`) and mode (`headless`/`interactive`/`consult`) labels.
3. **Decide** via a host one-shot handed the ranked candidates, budget, and outcomes; it
   answers `DISPATCH: <numbers>`/`none`, can only **narrow or hold**, and **fails open to
   rank** (#346) on an unclear read.
4. **Dispatch** the chosen set via the native engineer carry (`agent.<mode>.engineer`).
5. **Sleep** `--poll-interval`, **no LLM held open**.

Only the **headless** lane auto-dispatches; interactive/consult are surfaced.

## Drain → surface (ward#351, ward#353)

When the lane drains - **nothing queued or in flight** - director surfaces a **read-only
scope + dispatch session** on the lead repo ([agent-surface.md](agent-surface.md), the old
`architect` role); the init gate's "scope now" path reuses it. It blocks until the human
exits, then the heartbeat **resumes** if the queue refilled, else stops (ward#350).

## The WARD-OUTCOME marker (ward#310)

A detached engineer carry leads its retrospective with `WARD-OUTCOME: done` (or
`blocked`/`failed`); the loop reads only that line, and a no-marker exit is parked `failed`.

## Scope, ledger, trust

`--repo`/`--org` (ward#370) scope de-dupes across repos, defaulting to the cwd git origin; an
empty `--org` expansion errors. State lives in a per-repo YAML ledger under `~/.ward/backlog/`,
so a killed loop resumes, never re-dispatching. Dispatch is refused unless every scope repo is
a trusted owner.

## Flags

- `--repo a/b,c/d` scope; `--org <org>` repo-list scope (ward#370); `--max-parallel N` (2);
  `--triage`/`--no-triage` (startup triage, **on by default**; ward#397); `--limit` (50);
  `--poll-interval` (30s); `--max-cycles` (0=until drained); `--dry-run`.
- `--driver` (claude) drives director's OWN session (decision one-shot + drain surface);
  `--engineer-driver` overrides the dispatched-engineer harness, else inherits it (ward#355).
- Container/harness parity (ward#355): `--image`/`--tag`, `--ward-source`/`--ward-version`,
  `--aws`, `--tailnet`, `--no-pull`, `--with-repo`, `--print`, `--force`. The dispatch subset
  propagates into each engineer; the full set configures the surface.
  `--branch`/`--no-preflight` and `--watch`/`--detach` are absent (ward#350).

## Reservation conflicts defer (ward#352)

A dispatch onto a held reservation (another run, 2h TTL) is **deferred**, not failed: left
eligible, retried later. Only a real launch error parks `failed`; `--force` reclaims a
stale/foreign hold.

## See also

- [docs/agent.md](agent.md) - the `ward agent` roster + `warded` face.
- [docs/agent-surface.md](agent-surface.md) - the read-only surface it drops into.
- [docs/agent-engineer.md](agent-engineer.md) - the carry it dispatches.
