---
doc_goal: Describe the brokered launch contract that carries the caller's resolved ward version into a host-side dispatch.
---
# ward agent dispatch broker

`ward agent` and `warded` can forward a launch through the host dispatch broker
when the run is read-only or otherwise brokered.

## Contract

- The brokered request carries the caller's resolved ward version.
- A released caller binary forwards its own version when no explicit
  `--ward-version` pin is set.
- The brokered launch output reports the effective ward version it will use.
- The reservation seed context records that same effective version.
- A successful broker response means **the broker accepted the request, wrote a
  dispatch artifact, and started its host Ward launch worker**. It does not mean
  that a container is visible or that an engineer harness is running.

## Launch milestones

The broker logs these distinct milestones so a director does not have to infer
more from a successful forward than it promises:

1. **Broker accepted**: request shape, token, and transport passed, and the
   dispatch artifact path plus request ID now exist.
2. **Host Ward launch started**: the broker-owned host `ward agent` launch
   worker has begun. This is the detach point and the successful response
   boundary for a forwarded engineer launch.
3. **Container visible**: later host launch work created an engineer container
   that `ward agent list` can observe. It is recorded in the dispatch artifact;
   it is not awaited by the forwarding director command.
4. **Engineer harness started**: the harness starts inside that container. It
   is a still-later in-container milestone, visible through `ward agent logs`,
   and is never implied by broker acceptance or container visibility.

If a later milestone fails, the broker finalizes the already-created dispatch
artifact and uses the normal issue failure reporting. Recover with `ward agent
logs <owner/repo#N>` or `ward agent list` rather than keeping the director
command in the foreground.

## What this is for

- keeping a newer caller from silently falling back to a stale host default.
- making the launch version visible before the engineer container starts.
- keeping brokered dispatch separate from harness install.
- carrying the request-shape checks that happen before a launch is forwarded, and feeding the driftable rows in [agent-check-placement.md](agent-check-placement.md) back through the host launch path.

## See also

- [agent-director.md](agent-director.md) - the read-only director lane.
- [agent-ops.md](agent-ops.md) - the brokered operational surfaces.
- [agent-pr-workflow.md](agent-pr-workflow.md) - the native PR-workflow actions the broker serves.
