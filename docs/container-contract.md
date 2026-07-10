---
doc_goal: Collapse the container API, env, and permission contract into a single durable reference for the ephemeral run box.
---
# ward container contract

The container contract is small.

- The workspace clone is the run's working tree.
- The container gets only the mounts and env it needs.
- The entrypoint controls the runtime path from launch to teardown.

## What the contract covers

- `WARD_*` environment variables.
- bind mounts and read-only surfaces.
- the permission shape the container itself can use.
- the per-harness context level.

## How to read it

- `WARD_*` values are launch-time inputs, not repo config.
- mounts tell you what the container can touch.
- permissions tell you what the container can do.
- context level tells you how much doctrine the harness gets.

The contract is the boundary between the host and the run. If a value needs to
change the container's behavior, it belongs here or in the launch docs, not in
the repo's `.ward/ward.yaml`.

## What it does not cover

- feature work belongs in the agent docs.
- host-side fleet policy belongs in operator docs.
- repo policy belongs in `.ward/ward.yaml`.

## See also

- [container.md](container.md) - the overview.
- [container-lifecycle.md](container-lifecycle.md) - launch and teardown.
- [container-substrate.md](container-substrate.md) - `/substrate` and multi-repo.
