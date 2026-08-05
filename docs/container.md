---
doc_goal: Keep the ephemeral container model readable as a single page after the surrounding issue slices were collapsed away.
---
# ward container

The container subsystem is the box that makes the agent half real.

- one container per run.
- a fresh clone inside the box.
- least access by default.
- teardown is part of the contract, not an afterthought.

## The important bits

- The workspace clone stays on disk only inside the container.
- Read-only substrate checkouts can be mounted beside it.
- Host launch assets and the short-lived credential env-file share a hidden,
  platform-correct staging root with an operator-local override.
- Engineer and QA env files carry the separate `WARD_FORGEJO_GIT_TOKEN` value
  as in-container `FORGEJO_TOKEN` for Git only. Their tracker API reads and
  typed writes use the role-bound dispatch broker capability.
- The container is the wall that carries the feature from start to merge.

## What the box does

- it isolates the run from the host shell.
- it gives the agent a writable workspace without handing it the whole host.
- it lets the director read or stop a run without turning the host into a shell.
- it keeps the teardown rules close to the launch rules.

## What the box does not do

- it does not replace repo policy.
- it does not replace the agent workflow.
- it does not make a bad target safe.

## The release-era shape

The old docs had separate pages for API, env, permissions, reaping, debug, and
multi-repo behavior. Those details are now grouped into the smaller follow-on
docs below so the overview can stay short.

## See also

- [container-contract.md](container-contract.md) - mounts, env, permissions.
- [container-lifecycle.md](container-lifecycle.md) - launch and teardown.
- [container-substrate.md](container-substrate.md) - `/substrate` and grants.
