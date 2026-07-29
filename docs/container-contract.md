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

## Host staging

Per-run assets and the credential-bearing Docker env-file share one
platform-correct host root. Operator overrides, Windows ACL verification,
drive-sharing validation, and old profile-root migration are specified in
[container-staging.md](container-staging.md).

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

`WARD_READONLY=1` starts the privileged Compose broker and exports
`WARD_DISPATCH_BROKER_ADDR` plus a per-stack capability to the director.
`FORGEJO_TOKEN` exists only in the broker service environment. It is absent
from the director container environment, dropped process, argv, and projected
home. Ward's native Forgejo client sends only explicitly allowlisted request
shapes through the broker. See [broker.md](broker.md) for authorization and
failure behavior.

## What it does not cover

- feature work belongs in the agent docs.
- host-side launch preferences belong in operator YAML.
- repo policy belongs in `.ward/ward.yaml`.

## See also

- [container.md](container.md) - the overview.
- [container-lifecycle.md](container-lifecycle.md) - launch and teardown.
- [container-substrate.md](container-substrate.md) - `/substrate` and multi-repo.
- [context-bundle.md](context-bundle.md) - the optional context and tool handoff.
