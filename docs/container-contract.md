---
doc_goal: Collapse the container API, env, and permission contract into a single durable reference for the ephemeral run box.
---
# ward container contract

The container contract is small.

- The workspace clone is the run's working tree.
- The container gets only the mounts and env it needs.
- The entrypoint controls the runtime path from launch to teardown.
- Claude in Chrome stays explicitly disabled in the Claude Code container
  harness baseline.

## What the contract covers

- `WARD_*` environment variables.
- the read-only director's Compose broker address and its credential boundary.
- bind mounts and read-only surfaces.
- the optional read-only generic context-bundle handoff.
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

## Context bundle

`--context-bundle <path>` adds one explicit, read-only context input. Ward
validates its strict role-bound manifest and path allowlist before Docker
starts, mounts it at `/opt/ward-context-bundle`, and exports that fixed path as
`WARD_CONTEXT_BUNDLE`. Container startup revalidates the bundle and projects
the selected agent layout into the private agent home before launch.

An optional validated `bin/` is exposed as `WARD_CONTEXT_TOOLS` and appended
after the image's existing `PATH`. The bundle changes context and tool
availability only. It grants no authority or runtime capability. See
[context-bundle.md](context-bundle.md) for the full schema, ownership, and
failure contract.

## Read-only director credentials

`WARD_READONLY=1` starts the root Forgejo credential broker and exports its
group-readable `WARD_BROKER_SOCK` to the dropped director. The socket is a
capability, not a credential: `FORGEJO_TOKEN` is omitted from the director
process and remains root-only for the broker/reaper. See [broker.md](broker.md)
for its authorized operations and rotation recovery.

## What it does not cover

- feature work belongs in the agent docs.
- host-side fleet policy belongs in operator docs.
- repo policy belongs in `.ward/ward.yaml`.

## See also

- [container.md](container.md) - the overview.
- [container-lifecycle.md](container-lifecycle.md) - launch and teardown.
- [container-substrate.md](container-substrate.md) - `/substrate` and multi-repo.
- [context-bundle.md](context-bundle.md) - the optional context and tool handoff.
