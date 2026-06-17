# ward container reap

`ward container reap` is the deterministic teardown backstop for
[`ward container`](container.md). A container is throwaway: once it goes down,
anything not pushed to the remote is gone. The usual harness hedge - "left it on
a worktree, uncommitted, for review" - therefore silently loses work, and a
headless permission gate cannot be talked out of by prompt escalation. So the
no-lost-work guarantee does **not** live in the agent. It lives here.

## How it runs

The entrypoint arms `reap` as a `trap ... EXIT` and does **not** `exec` the
agent, so the reaper fires on every exit path - clean finish, crash, or Ctrl-C.
By the time it runs, the agent's permissions and disposition are out of the loop,
so nothing the agent does (or refuses to do) can defeat it. Normally automatic;
runnable by hand for debugging via `ward container exec <name> -- ward container reap`.

## What it does

1. Stages and commits anything the agent left loose (`git add -A` + a
   `--no-verify` residual commit - the goal is to preserve work, not re-gate it).
2. Fetches origin and integrates onto the latest `main` (a `rebase`; a conflict
   routes to salvage).
3. Scans the residual diff for content that should never silently land on `main`:
   vendored/generated trees (`node_modules`, `vendor`, `__pycache__`, `.venv`,
   `.next`, `.terraform`, `.gradle`, `target`), credential-shaped files (`.env`,
   `*.pem`/`*.key`, `id_rsa`, ...), and oversized or large binary blobs.
4. Decides deterministically:
   - clean diff + clean integration -> **push straight to `main`**.
   - anything else (conflict, scan finding, rejected push) -> **salvage**: push
     the work to a `ward-salvage/<id>` branch (durable), then file or append to a
     `[ward-salvage]` forgejo issue with recovery commands + findings (notification).

## Why this shape

Salvage is non-destructive, so any doubt routes to a branch rather than pushing
junk to `main` - a false-positive scan only parks clean work on a branch, never
discards it. The branch push comes before the issue, so a failed issue is a
missed notification, not lost work. If even the branch push fails (remote
unreachable), the reaper dumps the patch to the container log, recoverable via
`docker logs` before `ward container down`. It uses the container's
`FORGEJO_TOKEN` directly, so filing needs no `--aws`/SSM surface.

The agent's job is to make the reaper's job trivial: finish the feature, push to
`main` itself, leave a clean tree. The reaper is the backstop that makes the
guarantee real *without depending on the agent having done so*.

## See also

[docs/container.md](container.md) - the container subsystem.
[docs/FEATURES.md](FEATURES.md) - inventory.
