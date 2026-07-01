# ward agent director

`ward agent director` (public face `warded director`) is the **autonomous backlog
supervisor** role (ward#347, was `backlog`): it drives a repo's headless lane to drain.
ward#346 ported `backlog-loop.py`; ward#351 made it an LLM-in-the-loop heartbeat.

## Startup triage (ward#397)

Before the init gate, director folds in a **triage pass** (on by default, `--no-triage` skips)
that **writes** the tier + mode labels the heartbeat only read, warming the headless lane. See
[director-startup-triage.md](director-startup-triage.md).

## The init gate (ward#361)

At startup, **before the first drain tick**, director asks once - "drain the headless backlog
now?" **yes**/Enter begins the autonomous drain; **no** surfaces an interactive session first.
An opt-in (ward#350), asked **once at init**, never per tick; no terminal drains, and
`--dry-run`/`--print` skip it.

## The heartbeat (ward#351)

`director` is **attached/interactive only** - no `--detach` (runaway-dispatch risk). Each tick:

1. **Poll + reconcile** in-flight engineers: on exit read each `WARD-OUTCOME`, classify
   `done`/`blocked`/`failed`.
2. **Refresh** each ledger from the live backlog, ranking issues into lanes by tier
   (`P0`-`P4`) and mode (`headless`/`interactive`/`consult`).
3. **Decide** via a host one-shot over the ranked candidates; it answers `DISPATCH:
   <numbers>`/`none`, can only **narrow or hold**, and **fails open to rank** (#346).
4. **Dispatch** the chosen set via the engineer (`agent.<mode>.engineer`).
5. **Sleep** `--poll-interval`, **no LLM held open**.

Only the **headless** lane auto-dispatches; interactive/consult surface.

## Drain → surface (ward#351, ward#353)

When the lane drains - **nothing queued or in flight** - director surfaces a **read-only scope
+ dispatch session** (the old `architect` role; [agent-surface.md](agent-surface.md)),
blocking until the human exits; the heartbeat **resumes** if the queue refilled, else stops
(ward#350).

## The WARD-OUTCOME marker (ward#310)

A detached engineer leads its retrospective with `WARD-OUTCOME: done` (or `blocked`/`failed`).
The loop reads only that line; a no-marker exit is parked `failed`.

## Scope, ledger, trust

`--repo a/b,c/d` spans many repos (de-duped); `--org <org>` (ward#370) expands to every repo
that org owns, unioned with `--repo`; an empty expansion errors. State lives in a per-repo YAML
ledger under `~/.ward/backlog/` (a killed loop resumes, no issue re-dispatched); dispatch is
refused unless every scope repo is trusted.

**Config-stored default scope (ward#398).** With **neither `--repo` nor `--org`**, director
reads a `director.default-scope` list from `~/.ward/config.yaml` (each entry an **org** fanned
to its repos, or a bare `owner/name`) via the same union/de-dup/trust path; an absent key
falls back to the cwd origin, flags override. Host-owned, no default.

## Flags

- `--repo`/`--org` scope (ward#370); `--max-parallel N` (10); `--triage`/`--no-triage` (startup
  triage, on by default; ward#397); `--limit` (50); `--poll-interval` (30s); `--max-cycles`
  (0=drained); `--dry-run`. `--driver` (claude) drives director's OWN session;
  `--engineer-driver` overrides the engineer harness (ward#355).
- Container/harness parity (ward#355): `--image`/`--tag`, `--ward-source`/`--ward-version`,
  `--aws`, `--tailnet`, `--no-pull`, `--with-repo`, `--print`, `--force` - the dispatch subset
  reaches each engineer, the full set the surface. `--branch`/`--no-preflight`,
  `--watch`/`--detach` absent.

## Reservation conflicts defer (ward#352)

A dispatch onto a held reservation (another run, 2h TTL) **defers** - left eligible, retried
later; only a real launch error parks `failed`. `--force` reclaims a stale/foreign hold.

## See also

- [docs/agent.md](agent.md) - the `ward agent` roster + `warded` face.
- [docs/agent-surface.md](agent-surface.md) - the read-only surface it drops into.
- [docs/agent-engineer.md](agent-engineer.md) - the engineer it dispatches.
