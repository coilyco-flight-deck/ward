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

Engineer and QA receive `WARD_FORGEJO_GIT_TOKEN` as their in-container Git
credential and use a role-bound broker capability for tracker operations.
Director receives its harness credential, `WARD_DISPATCH_BROKER_ADDR`, and a
master capability. The broad forge token exists only in the broker process.

## Instructions and skills

Bootstrap creates the selected harness's native instruction file and skill
root in its private home. Compiled Ward doctrine and any validated context
bundle are projected there. Host-installed or host-converged skills and hooks
do not cross the container boundary.

## Permissions

Roles and context cannot change mounts, environment, credentials, network,
broker grants, or merge authority. Typed launch code fixes those properties.
Harness-specific settings may change invocation or display behavior only.

Per-run assets and the credential env file share the secured root in
[container-staging.md](container-staging.md).

## See also

* [context-bundle.md](context-bundle.md) - optional context projection.
* [agent-dispatch-broker.md](agent-dispatch-broker.md) - broker credential boundary.
* [container-substrate.md](container-substrate.md) - filesystem layout.
