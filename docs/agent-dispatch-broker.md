---
doc_goal: Define the supervised broker's cluster identity, durable request lifecycle, credential boundary, typed authority, and launch milestones.
---
# Dispatch broker

A collaboration cluster starts one independently supervised Compose broker.
Its `<harness>-<ab12>` cluster id is the Compose project, lifecycle key, and
`ward.cluster` label. Repository metadata never identifies a cluster.

## Plan-only requests

`warded engineer <ref> --print` crosses the read-only director boundary as a
synchronous `plan` request. The host resolves the same issue, configuration,
harness, workflow, image, version, mounts, and command as a local preview, then
returns that rendered plan directly to the requesting terminal. The output
starts with `PLAN ONLY - no launch was accepted`.

A plan request has no request id and never enters launch admission. It creates
no request journal, dispatch artifact, peer admission, reservation, launch
assets, issue comment, container, or harness process. It also skips launch-only
backpressure, capacity, Docker-readiness, preflight, and recovery checks. Plan
errors return synchronously. A launch action carrying `--print` is rejected so
the preview cannot silently fall back into durable dispatch.

## Durable dispatch

A launch caller mints a request id before the first dial. The broker validates
the role, action, argv, capability, version, transport, and cluster, then
persists a token-stripped request journal and artifact under `~/.ward`.
Retrying the same id and launch shape returns the existing result. Reusing the
id with a different shape is rejected.

Public states are `queued`, `accepted`, `launching`, `running`,
`cleanup-needed`, `completed`, `blocked`, `failed`, and `interrupted`.
`accepted` means the broker persisted and started its Ward launch worker. It
does not mean a container is visible or a harness is running. Later milestones
are recorded separately and read through dispatch status, list, and logs.

## Credential boundary

Only the broker receives the broad forge credential. Engineer and QA launch
resolution receives a distinct Git-only credential and role-bound broker
capability. Ward rejects a missing Git token or one equal to the broad token.
Director receives its harness credential and master broker capability, never a
transferable forge token. The broker snapshots credentials at stack start and
requires a stack recycle after credential rejection.

## Typed authority

The broker is not a generic HTTP proxy. It rechecks authenticated automation
identity, capability role and agent id, owner and repository shape, request and
response size, native route allowlists, and tracker record kind. Engineer and
QA raw forge access is fixed-read only. Writes use typed issue, pull-request,
reservation, workflow, review, dispatch, or QA operations. QA can mint only QA
records. Approval requires the director's master capability and a trusted
human's exact intent snapshot.

The provider-neutral [release contract](agent-release.md) stamps Director and
Ops identities and records immutable candidates, transaction phases, typed
attestations, outcomes, and evidence digests. It grants no operation authority.

## Supervision and teardown

Docker supervises the broker with `restart: unless-stopped`. Closing an
attached director removes only that director. Broker journals, dispatch
artifacts, and cluster state remain on the independent `~/.ward` mount. Ward
removes temporary launch env files when the attached run ends.

## Read surfaces

Use `ward agent dispatch list`, `dispatch status <request-id>`, `ward agent
list`, and `ward agent logs <request-id>`. Terminal records persist until an
explicit confirmed prune.

## See also

* [agent-dispatch-health.md](agent-dispatch-health.md) - restart decisions.
* [agent-clusters.md](agent-clusters.md) - cluster lifecycle.
* [agent-pr-workflow.md](agent-pr-workflow.md) - typed PR operations.
