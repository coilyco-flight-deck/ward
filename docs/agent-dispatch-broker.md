---
doc_goal: Describe the independently supervised broker that accepts durable director dispatches and launches sibling workers.
---
# ward agent dispatch broker

`ward agent` and `warded` can forward a launch through the supervised dispatch broker
when the run is read-only or otherwise brokered. A director stack starts the
broker as a long-lived Compose service at `broker:7420`, then starts the
director as a regular Compose service on the same project network and attaches
the terminal to it. Docker Desktop therefore groups both services under the
same Compose application.

Every new collaboration cluster receives one stable id in the form
`<harness>-<ab12>`, such as `codex-ab45`. That id is the Compose project name,
the `ward.cluster` label, and the lifecycle key shared by the broker, optional
director, directly attached peers, and nested peers. Repository, issue, role,
workflow, harness, and dispatch-request fields remain separate metadata. A
repository is never required to resolve a cluster.

## Contract

- The brokered request carries the caller's resolved ward version.
- A released caller binary forwards its own version when no explicit
  `--ward-version` pin is set.
- Brokered output and reservation seed context record the effective Ward version.
- Nested launches inherit `WARD_CLUSTER_ID` and the parent Compose network.
- Docker supervises the broker with `restart: unless-stopped`. Closing the
  director or its terminal removes only the director service. Ward does not run
  `compose down`, so the broker remains supervised.
- The broker persists token-stripped accepted requests and artifacts under
  `~/.ward`, which is mounted into the broker independently of its container
  writable layer.
- Native director Forgejo requests share this service under the credential and
  authorization contract in [broker.md](broker.md).
- The director mints a request ID before the first dial and reuses it when a
  response is lost. The same ID with the same launch shape returns the existing
  artifact. The same ID with different arguments is rejected.
- A successful broker response means **the broker accepted the request, wrote a
  durable request journal and dispatch artifact, and started its Ward launch
  worker**. It does not mean
  that a container is visible or that an engineer harness is running.

## Launch milestones

The broker logs these distinct milestones so a director does not have to infer
more from a successful forward than it promises:

1. **Broker accepted**: request shape, token, and transport passed, and the
   dispatch artifact path plus request ID now exist.
2. **Broker Ward launch started**: the broker-owned `ward agent` child process
   has begun. This is the detach point and the successful response
   boundary for a forwarded engineer launch.
3. **Container visible**: later broker launch work created a sibling container.
   Engineer visibility remains recorded in the dispatch artifact and exposed by
   `ward agent list`. Generic runs keep their normal container identity.
4. **Harness started**: the harness starts inside that container. It
   is a still-later in-container milestone, visible through `ward agent logs`,
   and is never implied by broker acceptance or container visibility.

If a later milestone fails, the broker finalizes the already-created dispatch
artifact and uses the normal issue failure reporting. Recover with `ward agent
logs <owner/repo#N>` or `ward agent list` rather than keeping the director
command in the foreground.

## What this is for

- keeping a newer caller from silently falling back to a stale host default.
- making the launch version visible before the engineer container starts.
- keeping brokered dispatch separate from the director and terminal lifecycles.
- keeping Forgejo credentials outside the director container and home.
- carrying the request-shape checks that happen before a launch is forwarded,
  and feeding the driftable rows in
  [agent-check-placement.md](agent-check-placement.md) back through the broker
  launch path.

## See also

- [agent-director.md](agent-director.md) - the read-only director lane.
- [agent-peer-collaboration.md](agent-peer-collaboration.md) - generic peers and messages.
- [agent-dispatch-recovery.md](agent-dispatch-recovery.md) - request journals
  and restart decisions.
- [agent-ops.md](agent-ops.md) - the brokered operational surfaces.
- [agent-pr-workflow.md](agent-pr-workflow.md) - the native PR-workflow actions the broker serves.
