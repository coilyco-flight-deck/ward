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

## What this is for

- keeping a newer caller from silently falling back to a stale host default.
- making the launch version visible before the engineer container starts.
- keeping brokered dispatch separate from harness install.

## See also

- [agent-director.md](agent-director.md) - the read-only director lane.
- [agent-ops.md](agent-ops.md) - the brokered operational surfaces.
- [agent-pr-workflow.md](agent-pr-workflow.md) - the native PR-workflow actions the broker serves.
