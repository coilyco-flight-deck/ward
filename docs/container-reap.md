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
3. Checks for **nothing to reap** *first*: a clean tree with `HEAD`
   already in `origin/main` is done, before the salvage gates, which read the
   then-empty `origin/main..HEAD` and would else false-salvage a landed run.
4. Verifies the carried issue has the same-repo `closes #N` reference. Missing
   reference means salvage, not push.
5. Integrates onto the latest `main` (`rebase`; conflicts route to salvage).
6. Scans the residual diff for junk that should never land on `main`: vendored
   trees (`node_modules`, ...), credential files (`.env`, `*.pem`, ...), blobs.
7. Decides deterministically:
   - clean diff + clean integration -> **push straight to `main`**.
   - anything else (conflict, scan finding, rejected push) -> **salvage**: push to
     a `ward-salvage/<id>` branch (durable), then notify - a **carried**
     run comments the notice back on its issue and **reopens** it; a **freeform**
     run files exactly **one** standalone `[ward-salvage]` issue, never appended.
8. Verifies each `--repo` grant landed: reads `WARD_EXTRA_REPOS`,
   checks the closing-reference discipline, and reopens the issue if any grant
   did not reach `origin/main`.

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

## Operator note: don't rotate the PAT mid-run

The container's `FORGEJO_TOKEN` is baked in at `ward agent` bring-up and frozen
for the container's life - the reaper reuses it, never re-resolving from SSM. So
**rotating or revoking the Forgejo PAT while a container is in flight** leaves it
carrying a dead token: the push to `main` fails on auth, routes to salvage, the
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

## See also

[docs/container.md](container.md) - container subsystem.
[docs/FEATURES.md](FEATURES.md) - inventory.
