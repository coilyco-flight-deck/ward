---
doc_goal: Let an operator run and reason about the director as ward's autonomous backlog supervisor - the LLM-in-the-loop heartbeat that drains a repo's headless lane under trust and slot bounds - not mistake it for a one-shot command.
---
# ward agent director

`ward agent director` (public face `warded director`) is the **autonomous backlog
supervisor** role: it drives a repo's headless lane to drain. It is an
LLM-in-the-loop heartbeat.

## Startup triage

Before the init gate, director folds in a **triage pass** (on by default, `--no-triage` skips)
that **writes** the tier + mode labels the heartbeat only read, warming the headless lane. See
[director-startup-triage.md](director-startup-triage.md).

## The init gate

At startup, **before the first drain tick**, director asks once - "drain the headless backlog
now?" **yes**/Enter begins the autonomous drain, **no** surfaces an interactive session first.
An opt-in asked **once at init**, never per tick. `--dry-run`/`--print` skip it.

## The heartbeat

`director` is **attached/interactive only** - no `--detach` (runaway-dispatch risk). Each tick:

1. **Poll + reconcile** in-flight engineers: on exit read each `WARD-OUTCOME` (done/blocked/failed).
2. **Refresh** each ledger from the live backlog, ranking issues into lanes by tier
   (`P0`-`P4`) and mode (`headless`/`interactive`/`consult`).
3. **Probe** forge liveness (the top candidate's issue get) so a recovery reaches the decision.
4. **Decide** via a host one-shot over the candidates + forge-health; answers `DISPATCH:
   <numbers>`/`none`, can only **narrow or hold**, and **fails open to rank**.
5. **Dispatch** the chosen set via the engineer (`agent.<mode>.engineer`).
6. **Sleep** `--poll-interval`, **no LLM held open**.

Only the **headless** lane auto-dispatches; interactive/consult surface.

## Surface: drain + on-demand

On drain (nothing queued or in flight) director opens a **read-only** dispatch session,
resuming on refill else stopping. It also offers **on-demand** when a tick can't
schedule (slots full): [director-on-demand-surface.md](director-on-demand-surface.md).

## The WARD-OUTCOME marker

A detached engineer leads its retrospective with a `WARD-OUTCOME:` line; the loop reads only
that line, and a no-marker exit is parked `failed`.

## Scope, ledger, trust

`--repo a/b,c/d` spans many repos (de-duped); `--org <org>` expands to every repo it
owns, unioned with `--repo` (empty expansion errors). State lives in a per-repo YAML ledger under
`~/.ward/backlog/` (killed loops resume, no re-dispatch); dispatch needs every scope repo trusted.

**Config-stored default scope.** With neither `--repo` nor `--org`, director reads
`director.default-scope` from `~/.ward/config.yaml` (each entry an **org** or bare `owner/name`)
via the same union/de-dup/trust path; an absent key falls back to the cwd origin. Host-owned.

## Flags

- `--repo`/`--org` scope; `--max-parallel N` (10); `--triage`/`--no-triage` (on by
  default); `--limit` (50); `--poll-interval` (30s); `--max-cycles` (0=drained);
  `--dry-run`. `--harness` (claude) drives director's session; `--engineer-harness` overrides it.
  Hidden `--engineer-driver` stays an alias.
- Container/harness parity: `--image`/`--tag`, `--ward-source`/`--ward-version`,
  `--aws`, `--tailnet`, `--no-pull`, `--with-repo`, `--print`, `--force` - the dispatch subset
  reaches each engineer, the full set the surface; `--branch`/`--no-preflight`/`--watch`/`--detach` absent.

## Dispatch-error disposition

Only a coded per-issue decline parks `failed`; a conflict or launch/infra failure defers and
retries, and a **livelock guard** breaks a stale-infra hold on a live-ok forge:
[agent-director-dispatch.md](agent-director-dispatch.md).

## See also

- [docs/agent.md](agent.md) - the `ward agent` roster + `warded` face.
- [docs/agent-surface.md](agent-surface.md) - the read-only surface it drops into.
- [docs/agent-engineer.md](agent-engineer.md) - the engineer it dispatches.
