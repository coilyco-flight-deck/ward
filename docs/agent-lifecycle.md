---
doc_goal: Compress the launch path into one release-era guide so a reader can tell what has to be true before a run starts and what is recorded when it does.
---
# ward agent lifecycle

The launch path is short and explicit.

1. Resolve the issue or ref.
2. Run the harness install step and verify the selected binary is available.
3. Run the preflight that matches the role and harness.
4. Post or refresh the reservation comment.
5. Launch the ephemeral container.
6. Hand off to the selected workflow.

## What the launch path enforces

- The target must be trusted for the selected forge policy.
- A reserved issue stays reserved until the run finishes or times out.
- The repo must still have room under the three-engineer working cap, which
  composes with the open-PR backpressure gate.
- A missing harness binary or failed install aborts before the run starts.
- The run writes one auditable trail, not a silent shell session.
- `--print` shows the launch without starting it.

## Credentials and context

Host-side credentials are resolved before the container starts. The run then
inherits the selected harness context level and mount set.

## Common launch checks

- issue ownership matches the configured trust list.
- the repo is the expected repo.
- the harness install hook completed successfully and left the binary on PATH.
- the selected harness can reach its credential source or endpoint.
- the reservation comment still belongs to this run.

If any of those checks fail, the launch should stop before the container does
real work.

## What gets recorded

- the resolved ref.
- the role and harness.
- the selected workflow.
- the container identity.
- the final outcome.

That record is what the troubleshooting docs and the issue thread use when a
run needs to be explained after the fact.

## See also

- [first-run.md](first-run.md) - the first dry run.
- [agent-harnesses.md](agent-harnesses.md) - harness differences.
- [agent-roles.md](agent-roles.md) - which role does what.
- [agent-workflow.md](agent-workflow.md) - how the run lands.
