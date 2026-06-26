# ward agent director

`ward agent director` (public face `warded director`) is the **autonomous backlog
supervisor** role (ward#347, was `backlog`): it drives a repo's headless lane to drain via
ward's own internals. ward#346 ported agentic-os's `backlog-loop.py`; ward#351 made it an
**LLM-in-the-loop heartbeat** that surfaces an interactive session on drain.

## The heartbeat (ward#351)

`director` is **attached/interactive only** - there is no `--detach` (runaway-dispatch
risk). Each tick:

1. **Poll + reconcile** in-flight engineers: on exit read each `WARD-OUTCOME` comment and
   classify the run `done` / `blocked` / `failed`.
2. **Refresh** each ledger from the live backlog, ranking issues into lanes by tier
   (`P0`-`P4`) and mode (`headless`/`interactive`/`consult`) labels.
3. **Decide** via a host one-shot (`claude -p` / `goose run -t`) handed the ranked candidates,
   the budget, the in-flight set, and recent outcomes; it answers on a `DISPATCH: <numbers>` /
   `DISPATCH: none` line. It can only **narrow or hold** rank, and **fails open to rank** (the
   #346 floor) on any unclear/failed read.
4. **Dispatch** the chosen set via the native engineer carry (audited `agent.<mode>.engineer`).
5. **Sleep** `--poll-interval` with **no LLM held open**.

Only the **headless** lane is auto-dispatched; interactive/consult issues are surfaced.

## Drain → surface (ward#351)

When the lane drains - **nothing queued and nothing in flight** - the director does not exit.
It reports the disposition and surfaces a **read-only architect session** on the scope's lead
repo for new direction. That blocks until the human exits; the heartbeat then **resumes** if
the queue refilled, else stops. With no terminal it cannot surface, so a drain there exits
cleanly (ward#350 will collapse the standalone `architect` into this phase).

## The WARD-OUTCOME marker (ward#310)

A detached engineer carry leads its closing retrospective with one line - `WARD-OUTCOME: done
- <what landed>` (or `blocked` / `failed - <why>`). Baked into the seed; the loop reads only
that line. A container that exits with no marker is parked `failed`.

## Scope, ledger, trust

`--repo a/b,c/d` (comma-separated, de-duped) spans many repos; the default is the cwd git
origin. State lives in a durable per-repo YAML ledger under `~/.ward/backlog/`, so a killed
loop resumes and a dispatched issue is never re-dispatched. Dispatch is refused unless every
scope repo is a trusted owner; one audit row per run.

## Flags

- `--repo a/b,c/d` scope; `--max-parallel N` (2); `--triage`; `--limit` (50); `--poll-interval`
  (30s); `--max-cycles` (0=until drained); `--dry-run`.
- `--driver` (claude) drives director's OWN session (the decision one-shot + drain surface);
  `--engineer-driver` overrides the dispatched-engineer harness, else they inherit `--driver`
  (ward#355, two-level).
- Container/harness parity with engineer + architect (ward#355): `--image`/`--tag`,
  `--ward-source`/`--ward-version`, `--aws`, `--host-net`/`--ts-sidecar`, `--no-pull`,
  `--with-repo`, `--print`, `--force`. The dispatch-configuring subset (`--driver`/
  `--engineer-driver`, `--image`, `--tag`, `--aws`, `--host-net`, `--ts-sidecar`,
  `--ward-version`) propagates into each engineer; the full set configures the drain surface.
  `--print` shows that plan, launches nothing. `--branch`/`--no-preflight` and
  `--watch`/`--detach` are absent (ward#350).

## Reservation conflicts defer (ward#352)

A dispatch onto a held reservation (another run holds it, 2h TTL) is **deferred**, not failed:
left `queued`/eligible, retried later, off the failure tally. Only a real launch error (pull,
create) parks `failed`. `--force` makes the engineers reclaim a stale/foreign hold.

## See also

- [docs/agent.md](agent.md) - the `ward agent` roster + `warded` face.
- [docs/agent-subcommands.md](agent-subcommands.md) - the roles, including this one.
- [docs/agent-engineer.md](agent-engineer.md) - the carry the director dispatches.
