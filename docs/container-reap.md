# ward container reap

`ward container reap` is the deterministic teardown backstop for
[`ward container`](container.md). A container is throwaway: once it goes down,
anything not pushed is gone, so the usual harness hedge - "left it uncommitted,
for review" - silently loses work. The no-lost-work guarantee lives here, not in
the agent.

## How it runs

The entrypoint arms `reap` as a `trap ... EXIT` and does **not** `exec` the
agent, so the reaper fires on every exit path - clean finish, crash, or Ctrl-C.
By the time it runs, the agent's permissions are out of the loop, so nothing it
does can defeat it. It is a hidden (ward#263) verb the entrypoint calls.

## What it does

1. Stages and commits anything the agent left loose (`git add -A` + a
   `--no-verify` residual commit - the goal is to preserve work, not re-gate it).
2. Fetches origin and integrates onto the latest `main` (a `rebase`; a conflict
   routes to salvage).
3. Scans the residual diff for content that should never silently land on `main`:
   vendored/generated trees (`node_modules`, `vendor`, ...), credential-shaped
   files (`.env`, `*.pem`/`*.key`, `id_rsa`, ...), and oversized binary blobs.
4. Decides deterministically:
   - clean diff + clean integration -> **push straight to `main`**.
   - anything else (conflict, scan finding, rejected push) -> **salvage**: push
     the work to a `ward-salvage/<id>` branch (durable), then file or append to a
     `[ward-salvage]` forgejo issue with recovery commands + findings (notification).
5. Verifies each `--repo` grant landed (ward#291): reads `WARD_EXTRA_REPOS` and,
   per granted clone, checks its `HEAD` reached the freshly-fetched `origin/main`.
   An un-pushed grant is preserved on a `ward-salvage/<id>` branch and the target
   issue is **reopened** with a recovery comment (undoing any `closes #N`). The
   reaper still never pushes a grant to `main` - it only verifies and surfaces.

## Why this shape

Salvage is non-destructive, so any doubt routes to a branch rather than pushing
junk to `main` - a false-positive scan only parks clean work, never discards it.
The branch push comes before the issue, so a failed issue is a missed
notification, not lost work. If even the branch push fails (remote unreachable),
the reaper dumps the patch to the container log, recoverable via `docker logs`
while it survives the keep-10 sweep ([container-cleanup.md](container-cleanup.md)).

The agent's job is to make the reaper's trivial: finish the feature, push to
`main`, leave a clean tree. The reaper is the backstop that makes the guarantee
real *without depending on the agent having done so*.

## Operator note: don't rotate the PAT mid-run

The container's `FORGEJO_TOKEN` is `/forgejo/api-token` baked in at `ward agent`
bring-up and frozen for the container's life - the reaper reuses it, never
re-resolving from SSM. So **rotating or revoking the Forgejo PAT while a container
is in flight** leaves it carrying a dead token: the push to `main` fails on auth,
routes to salvage, the salvage branch push fails on the same token, and the work
falls through to the container-log recovery path (`docker logs <name>`). Work is
preserved but recovery is manual. Before rotating, let in-flight runs finish.

So an auth-cause salvage reads distinct from a conflict (ward#103), the reaper
classifies the push: credential-rejection markers (`Authentication failed`,
`403`/`401`, ...) report `reasonAuthFail`, not the misleading "remote advanced"
race, and the issue gains a "Likely cause: dead/rotated PAT" section. Each issue
stamps container uptime at reap (the PAT snapshot's age, from `WARD_CONTAINER_UP`).
When the token is fully dead the issue can't be filed, so the log names the cause.

Host AWS/STS expiry is **not** a concern: AWS is touched only on the host at
bring-up to read the PAT from SSM, never during reap.

## See also

[docs/container.md](container.md) - container subsystem.
[docs/FEATURES.md](FEATURES.md) - inventory.
