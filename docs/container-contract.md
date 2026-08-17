---
doc_goal: Define the complete host-to-container contract for mounts, environment, credentials, permissions, context, and skill projection.
---
# Container contract

## Writable state

The target clone, private harness home, caches, `/scratch`, and run state are
writable inside the run. Additional writable repositories require an explicit
workflow grant. A read-only director clone has a disabled push URL and cannot
land local commits.

## Read-only inputs

Ward mounts staged entrypoint and doctrine, shared substrate, optional context
bundle and `/refs` repositories, and selected host inputs read-only. The
container never inherits the operator's whole home, host checkout, hooks, or
harness configuration.

## Environment and credentials

`WARD_*` values are launch-time runtime inputs, not repository config. Ward
uses a short-lived secured env file for credential handoff and does not render
credentials into Compose YAML, argv, printable environment, or audit rows.
After the selected adapter consumes its bootstrap channel, Ward removes every
known harness credential channel before launching the dropped child process.

Engineer launches ask Git to resolve author and committer identity before
admission, then project those exact resolved values into the private container.
`WARD_GIT_NAME` and `WARD_GIT_EMAIL` are optional explicit fallback inputs.
Container system policy sets `user.useConfigOnly=true`, never writes
`user.name` or `user.email`, and lets Git reject a missing identity before a
commit is created. Harness display identity and authenticated forge actors do
not supply Git commit identity.

Engineer and QA receive `WARD_FORGEJO_GIT_TOKEN` as their in-container Git
credential and use a role-bound broker capability for tracker operations.
Director receives its harness credential, `WARD_DISPATCH_BROKER_ADDR`, and a
master capability. The broad forge token exists only in the broker process.

## Instructions and skills

Bootstrap creates the selected harness's native instruction file and skill
root in its private home. It reads only instruction names accepted by that
adapter, composes in memory, and atomically writes a regular file directly to
the native load point. There is no shared `~/AGENTS.md`, sibling load-point
fanout, on-disk append step, or fallback to another harness's instruction.

A validated context bundle's selected-role instruction is authoritative. Ward
adds only its compiled container authority and safety constraints in memory,
then writes the same native load point. Host-installed or host-converged skills
and hooks do not cross the container boundary.

## Permissions

Roles and context cannot change mounts, environment, credentials, network,
broker grants, or merge authority. Typed launch code fixes those properties.
Harness-specific settings may change invocation or display behavior only.
Bootstrap invokes only capabilities implemented by the selected adapter.
Claude alone receives Claude permission settings. Before launch, Ward changes
ownership for the workspace and existing paths in the selected adapter's
declared surface. It does not recursively claim sibling harness homes.

Per-run assets and the credential env file share the secured root in
[container-staging.md](container-staging.md).

## See also

* [context-bundle.md](context-bundle.md) - optional context projection.
* [agent-dispatch-broker.md](agent-dispatch-broker.md) - broker credential boundary.
* [container.md](container.md) - filesystem layout.
