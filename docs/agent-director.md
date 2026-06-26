# ward agent director

`ward agent director` (public face `warded director`) is the **autonomous backlog
supervisor** role (ward#347, was `backlog`): it drives a repo's headless lane to drain via
ward's own internals. ward#346 ported `backlog-loop.py`; ward#351 made it an **LLM-in-the-
loop heartbeat** that surfaces a read-only session on drain.

## The init gate (ward#361)

At startup, **before the first drain tick**, director asks once - "drain the headless
backlog now?" **yes** (or a bare Enter) begins the autonomous drain; **no** surfaces an
interactive session first, and a drain begins once headless work is queued. The opening
drain is a deliberate opt-in (ward#350: human in the loop). Asked **once at init**, never per
tick or on a later resume; no terminal drains (the default), and `--dry-run`/`--print` skip it.

## The heartbeat (ward#351)

`director` is **attached/interactive only** - no `--detach` (runaway-dispatch risk). Each
tick:

1. **Poll + reconcile** in-flight engineers: on exit read each `WARD-OUTCOME`, classify
   `done`/`blocked`/`failed`.
2. **Refresh** each ledger from the live backlog, ranking issues into lanes by tier
   (`P0`-`P4`) and mode (`headless`/`interactive`/`consult`) labels.
3. **Decide** via a host one-shot (`claude -p` / `goose run -t`) handed the ranked candidates,
   budget, and recent outcomes; it answers `DISPATCH: <numbers>`/`none`, can only **narrow or
   hold** rank, and **fails open to rank** (#346 floor) on any unclear read.
4. **Dispatch** the chosen set via the native engineer carry (`agent.<mode>.engineer`).
5. **Sleep** `--poll-interval`, **no LLM held open**.

Only the **headless** lane auto-dispatches; interactive/consult are surfaced.

## Drain → surface (ward#351, ward#353)

When the lane drains - **nothing queued or in flight** - director does not exit: it reports
the disposition and surfaces a **read-only scope + dispatch session** on the lead repo - the
old `architect` role, folded in here ([agent-surface.md](agent-surface.md)); the init gate's
"scope now" path uses the same surface. It blocks until the human exits; the heartbeat
**resumes** if the queue refilled, else stops. No terminal exits cleanly (ward#350).

## The WARD-OUTCOME marker (ward#310)

An engineer carry closes with `WARD-OUTCOME: done - <what landed>` (or `blocked` / `failed -
<why>`). The loop reads only that line; a no-marker exit is parked `failed`.

## Scope, ledger, trust

`--repo a/b,c/d` spans many repos (de-duped); default is the cwd git origin. State lives in
a per-repo YAML ledger under `~/.ward/backlog/`, so a killed loop resumes and an issue is
never re-dispatched. Dispatch is refused unless every scope repo is a trusted owner.

## Flags

- `--repo a/b,c/d` scope; `--max-parallel N` (2); `--triage`; `--limit` (50); `--poll-interval`
  (30s); `--max-cycles` (0=until drained); `--dry-run`.
- `--driver` (claude) drives director's OWN session (decision one-shot + drain surface);
  `--engineer-driver` overrides the dispatched-engineer harness, else inherits `--driver` (ward#355).
- Container/harness parity (ward#355): `--image`/`--tag`, `--ward-source`/`--ward-version`,
  `--aws`, `--host-net`/`--ts-sidecar`, `--no-pull`, `--with-repo`, `--print`, `--force`. The
  dispatch subset propagates into each engineer; the full set configures the surface. `--print`
  launches nothing. `--branch`/`--no-preflight` and `--watch`/`--detach` are absent (ward#350).

## Reservation conflicts defer (ward#352)

A dispatch onto a held reservation (another run, 2h TTL) is **deferred**, not failed: left
eligible, retried later, off the failure tally. Only a real launch error parks `failed`.
`--force` makes engineers reclaim a stale/foreign hold.

## See also

- [docs/agent.md](agent.md) - the `ward agent` roster + `warded` face.
- [docs/agent-surface.md](agent-surface.md) - the read-only surface it drops into on drain.
- [docs/agent-engineer.md](agent-engineer.md) - the carry it dispatches.
