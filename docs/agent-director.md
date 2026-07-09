doc_goal: Let an operator run and reason about the director as ward's autonomous backlog supervisor - the LLM heartbeat that drains a repo's headless lane under trust and slot bounds - not mistake it for a one-shot command.
---
# ward agent director

`ward agent director` (public face `warded director`) is the **autonomous backlog
supervisor** role: it drives a repo's headless lane to drain. It is an LLM loop.

## Startup triage

Before the init gate, director folds in a **triage pass** (on by default, `--no-triage` skips)
that writes the tier + mode labels the heartbeat reads. See
[director-startup-triage.md](director-startup-triage.md).

## The init gate

At startup, director polls once, then checks whether headless work is queued or in flight.
If empty, it skips the init gate and drains/surfaces.
If work remains, it asks whether to drain now. **yes**/Enter drains, **no** surfaces first.

## The heartbeat

`director` is **attached/interactive only** - no `--detach` (runaway-dispatch risk). Each tick:

1. **Poll + reconcile** in-flight engineers: on exit read each `WARD-OUTCOME`. `submitted` and `merge-ready` are nonterminal PR states.
2. **Refresh** each ledger from the backlog by tier (`P0`-`P4`) and mode
   (`headless`/`interactive`/`consult`), folding PRs into a `pull-request` lane `issue #N` / `PR #N`.
3. **Probe** forge liveness (the top candidate's issue get) so a recovery reaches the decision.
4. **Sweep** ward-owned PRs that carry the `pull-request-and-merge` marker. See [agent-director-pr-merge.md](agent-director-pr-merge.md).
5. **Decide** via a host one-shot over the candidates + forge-health; answers `DISPATCH:
   <numbers>`/`none`, can only **narrow or hold**, and **fails open to rank**.
6. **Dispatch** the chosen set via the engineer (`agent.<mode>.engineer`).
7. **Sleep** `--poll-interval`, **no LLM open**.

Only the **headless** lane auto-dispatches; interactive issues surface. The merge sweep is narrow and policy-bound. See [agent-director-pr-merge.md](agent-director-pr-merge.md).

## The WARD-OUTCOME marker

Engineer retrospectives lead with `WARD-OUTCOME:`; a no-marker exit is parked `failed`.

See [agent-director-merge.md](agent-director-merge.md).

## Scope, ledger, trust

`--repo a/b,c/d` spans many repos (de-duped); `--org <org>` expands to every repo it
owns, unioned with `--repo` (empty expansion errors). State lives in a per-repo YAML ledger under
`~/.ward/backlog/`; dispatch needs every scope repo trusted.

**Config-stored default scope.** With neither `--repo` nor `--org`, director reads
`director.default-scope` from `~/.ward/config.yaml` (each entry an **org** or bare `owner/name`)
via the same union/de-dup/trust path; an absent key falls back to the cwd origin. Host-owned.

## Flags

- `--repo`/`--org` scope; `--max-parallel N` (10); `--triage`/`--no-triage` (on by
  default); `--limit` (50); `--poll-interval` (30s); `--max-cycles` (0=drained); `--dry-run`.
  `--harness` (claude) drives director's session; `--engineer-harness` overrides it.
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
- [docs/agent-workflow.md](agent-workflow.md) - the run landing policy, including the director-owned merge boundary.
