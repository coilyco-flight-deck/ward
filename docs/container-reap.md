---
doc_goal: Give an operator full confidence in the deterministic teardown reaper as the no-lost-work backstop - how EXIT-trap arming makes it fire on every exit path beyond the agent's reach, the ordered land-or-salvage decision (empty-repo establish-main, nothing-to-reap, closing-ref, integrate, junk-scan, push-or-salvage, grant-verify), and the PAT-rotation and auth-classification caveats - so its land-or-salvage contract is trustable rather than opaque.
---
# ward container reap

`ward container reap` is the deterministic teardown backstop for
[`ward container`](container.md). A container is throwaway: once it goes down,
anything not pushed is gone. The no-lost-work guarantee lives here, not in the
agent.

## How it runs

The entrypoint arms `reap` as a `trap ... EXIT` and does **not** `exec` the
agent, so the reaper fires on every exit path - clean finish, crash, or Ctrl-C.
By the time it runs, the agent's permissions are out of the loop, so nothing it
does can defeat it. It is a hidden entrypoint-called verb.

## What it does

1. Stages and commits anything the agent left loose (`git add -A` + a
   `--no-verify` residual commit - the goal is to preserve work, not re-gate it).
2. Records dispatch-time run provenance. See [run provenance](container-reap-provenance.md).
3. Handles the **empty repo** (`origin/main` absent) as an **establish-main**
   case, not a salvage: a brand-new repo has no base branch to integrate onto, so
   a clean, run-owned commit is pushed **as** `main` - creating the default branch
   from the run's work, landing the feature and firing CI in one step
   ([ward#599](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/599)).
   The run-owned proof here is the committed `closes #N` (an empty repo has no
   stale history to mis-credit, so the `origin/main`-relative provenance proof does
   not apply); the junk scan runs against git's empty tree. Salvage stays reserved
   for a **real** failure - a workflow that does not land on main, junk in the tree,
   a missing closing reference, or a rejected push - never the benign "the repo was
   empty" condition.
4. Checks for **nothing to reap** *next*: a clean tree with `HEAD`
   already in `origin/main` is done, before the salvage gates, which read the
   then-empty `origin/main..HEAD` and would else false-salvage a landed run.
5. Verifies the carried issue has the same-repo `closes #N` reference. Missing
   reference means salvage, not push.
6. Integrates onto the latest `main` (`rebase`; conflicts route to salvage).
7. Scans the residual diff for junk that should never land on `main`: vendored
   trees (`node_modules`, ...), credential files (`.env`, `*.pem`, ...), blobs.
8. Decides deterministically:
   - clean diff + clean integration -> **re-checks the carried `closes #N` is in
     the exact post-rebase history about to land, then push straight to `main`**.
     This push-site re-check ([ward#515](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/515))
     co-locates the closing-ref invariant with the irreversible push, so a
     residual-only run whose sole landable commit is the reaper's own
     `ward-container: residual ... work on <slug>` commit (subject + attribution
     trailer, **no** `closes #N`) can never reach `main` even if a future
     reordering of the step-5 gate regresses - the ordering churn of
     [ward#513](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/513)/[ward#518](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/518)
     already broke that gate once.
   - anything else (conflict, scan finding, missing closing reference, rejected
     push) -> **salvage**: push to
     a `ward-salvage/<id>` branch (durable), then notify - a **carried**
     run comments the notice back on its issue and **reopens** it; a **freeform**
     run files exactly **one** standalone `[ward-salvage]` issue, never appended.
9. Verifies each `--repo` grant landed: reads `WARD_EXTRA_REPOS` and, for each
   grant, checks whether its work is present on the freshly-fetched
   `origin/main` - **content**, not `HEAD == origin/main` equality. A grant lands
   when either its local `HEAD` is **reachable from** `origin/main` (a plain or
   merge-commit landing leaves `HEAD` a proper ancestor - reachability, not
   equality, [ward#583](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/583))
   **or** every local commit ahead of `origin/main` already exists there **by
   patch-id** (`git cherry`). The patch check catches work that landed under a
   **different commit hash** - a change rebased or re-committed onto a busy
   `main`, or an identical block another run already pushed - which a
   HEAD-ancestry test alone false-flags as a phantom "1 local commit never
   reached origin/main", fabricating an empty salvage branch and a spurious
   reopen ([ward#587](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/587)).
   The reaper re-fetches across a short propagation window before declaring a
   miss (a just-landed push can lag its remote-tracking ref), and only reopens
   the issue when a grant's content genuinely did not reach `origin/main`.

## Why this shape

Salvage is non-destructive, so any doubt routes to a branch rather than pushing
junk to `main` - a false-positive scan only parks clean work, never discards it.
The branch push comes before the issue, so a failed issue is a missed
notification, not lost work. If even the branch push fails (remote unreachable),
the reaper dumps the patch to the container log, recoverable via `docker logs`
([container-cleanup.md](container-cleanup.md)).

The agent's job is to make the reaper's trivial: finish, push to `main`, leave a
clean tree. The reaper is the backstop that holds *without depending on the agent*.
On salvage or failure it also dumps a [reap diagnostics](container-reap-diagnostics.md) block so a bad outcome self-diagnoses.

## The gate is only as strong as the reaper's ward version

The closing-ref gate above (steps 5 and 8) runs **in-container**, from the ward
binary the container downloaded at dispatch. So the enforcement is only as fixed
as that binary: a container running a ward built **before** the closing-ref gate
([ward#511](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/511))
has no gate at all, and its reaper can push a residual commit to `main` without
the carried `closes #N` - exactly the
[infrastructure#427](https://forgejo.coilysiren.me/coilyco-flight-deck/infrastructure/issues/427)
incident that motivated [ward#515](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/515),
where the fix was already on `main` but a stale in-container reaper ran anyway.
No re-check the current binary adds can fix a binary that already shipped, so the
invariant is enforced one layer up, at **dispatch**: `buildUpPlan` refuses to
launch a container pinned to a ward strictly **older** than the dispatching host
([ward#529](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/529),
[agent-ward-downgrade.md](agent-ward-downgrade.md)), so a known-buggy reaper never
ships in the first place. Keep the dispatching host's ward current
(`brew upgrade coilyco-flight-deck/tap/ward`) and do not pass an older
`--ward-version` / `WARD_AGENT_VERSION` without `--allow-ward-downgrade`.

## Operator note: don't rotate the token mid-run

The container's `FORGEJO_TOKEN` is a snapshot of the `coilyco-ops` bot token
(SSM `/forgejo/coilyco-ops/api-token`, not a personal PAT - [ward#161](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/161)), baked in
at `ward agent` bring-up and frozen for the container's life - the reaper reuses
it, never re-resolving from SSM. So **rotating or revoking the bot's Forgejo
token while a container is in flight** leaves it carrying a dead token: the push
to `main` fails on auth, routes to salvage, the
salvage branch push fails on the same token, and the work falls through to the
container-log recovery path (`docker logs <name>`). Work is preserved but recovery
is manual. Before rotating, let in-flight runs finish.

So an auth-cause salvage reads distinct from a conflict, the reaper
classifies the push: credential-rejection markers (`Authentication failed`,
`403`/`401`, ...) report `reasonAuthFail`, not the misleading race, and the issue
gains a "Likely cause: dead/rotated PAT" section. A fully-dead token can't file
the issue, so the log names the cause.

Host AWS/STS expiry is **not** a concern: AWS is touched only on the host at
bring-up to read the PAT from SSM, never during reap.

## A pre-launch death names its gate ([ward#609](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/609))

When a container exits **before** launching the agent (the [ward#222](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/222) smoke gate,
an unreachable Ollama endpoint, or a bootstrap failure), the reaper retracts the
reservation with a release comment - and that comment now names the **specific
gate** that died, folds in the actual error line, and gives the recovery step,
so an operator diagnoses on the issue thread rather than in docker logs. The
entrypoint records the failing gate (`auth` / `ollama-probe` / `bootstrap`) to
`WARD_GATE_FAILURE_FILE` (default `/run/ward/gate-failure`); the reaper reads it in
`releaseReservationIfUnstarted`. A death with no recorded gate falls back to the
generic release comment. See [agent-reservation.md](agent-reservation.md).

The release comment is also **loud and machine-detectable** ([ward#595](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/595)): it leads
with a "⚠️ Run never started — this issue needs re-dispatch" headline and carries the
`<!-- ward-needs-redispatch -->` marker (`agentNeedsRedispatchMarker`), so an orphaned run
reads as a call to action, not a benign reservation-release that a human or a heartbeat
mistakes for "was dispatched, in flight". A `ward agent director` re-queues such an issue
deterministically (bounded by a re-dispatch cap, then parks it `blocked` as
`orphaned-needs-redispatch`); see [agent-director-dispatch.md](agent-director-dispatch.md).

## See also

[docs/container.md](container.md) - container subsystem.
[docs/FEATURES.md](FEATURES.md) - inventory.
