---
doc_goal: Compress the launch path into one release-era guide so a reader can tell what has to be true before a run starts and what is recorded when it does.
---
# ward agent lifecycle

The repository workflow launch path is short and explicit. Vocabulary lives in
[terminology.md](terminology.md).

1. Resolve the issue or ref.
2. Run the harness install step and verify the selected binary is available.
3. Run the preflight that matches the role and harness.
4. Post or refresh the reservation comment.
5. Launch the ephemeral container.
6. Hand off to the selected workflow.

A repository-free peer validates its cluster and role-bound bundle, confirms
the broker, then launches with read-only inputs and writable runtime paths. It
performs no repo inference, clone, allowlisting, reservation, workflow, or
Forgejo projection. `--repo owner/name` selects the workflow above.

## What the launch path enforces

- The target must be trusted for the selected forge policy.
- A reserved issue stays reserved until the run finishes or times out.
- The repo must still have room under the three-engineer working cap, which
  composes with the open-PR backpressure gate. Harness-native goals can dispatch
  across repositories while those launch-time limits remain authoritative.
- A missing harness binary or failed install aborts before the run starts.
- The run writes one auditable trail, not a silent shell session.
- `--print` shows the launch without starting it.

## Check placement

See [agent-check-placement.md](agent-check-placement.md) for the current broker-time vs pre-flight matrix.

- The driftable guards - reservation conflict, open-PR pressure, branch-state / continuation shape, and capacity - are re-read at launch time so queued work does not start against stale state.
- The broker handles request-shape and transport gating before the launch is forwarded.
- The pre-flight owns the issue-facing refusal path, the trust gate, and the host-side probes that can still fail after the queue wait.

## Credentials and context

Host-side credentials are resolved before the container starts. Engineer and
QA runs receive the existing Git and harness channels. A Compose director gets
only its selected harness channel and broker capability. Its sibling broker
alone receives the Forgejo credential. A repository-free collaboration peer
receives its harness channel and broker capability, with no Git or Forgejo
credential. Each run inherits its selected context level and mount set.

## Split-stack repositories

Tracker, checkout, and landing are separate. See [agent-split-stack.md](agent-split-stack.md).

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

Troubleshooting and the issue thread use that record to explain a run.

## See also

- [first-run.md](first-run.md) - the first dry run.
- [agent-harnesses.md](agent-harnesses.md) - harness differences.
- [agent-roles.md](agent-roles.md) - which role does what.
- [agent-workflow.md](agent-workflow.md) - how the run lands.
- [terminology.md](terminology.md) - lifecycle vocabulary and conceptual model.
