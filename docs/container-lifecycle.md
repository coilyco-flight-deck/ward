---
doc_goal: Give the ephemeral run box a single lifecycle guide covering launch, debug, stop, reap, and cleanup.
---
# ward container lifecycle

The container exists to carry one feature from start to merge.

1. Launch from the selected agent workflow.
2. Clone the target into the container workspace.
3. Run the detached work or the read-only director session.
4. Drain logs and post the result.
5. Reap or clean up the container.

## Teardown rules

- Clean runs land or merge, then disappear.
- Wedged runs are reaped by policy.
- Debugging starts from the container logs, not by rummaging in the host tree.

## Launch details

- the host stages the package's matching Linux ward binary for the entrypoint.
  Linux hosts copy the running executable. macOS and Windows packages carry a
  same-version Linux sidecar. Explicit version pins still use release assets,
  and older packages without a sidecar fall back to that download path.
- the target repo is cloned into the workspace inside the box.
- the selected workflow decides whether the run ends at a patch, a PR, or a
  merge.
- the teardown path always has to know whether the run actually landed.

## Related surfaces

- `stop` halts a specific run.
- `cleanup` removes stopped containers.
- `reap` is the backstop for idle engineer runs.

## Debug path

If the run looks stuck:

1. read the run logs.
2. check the reservation or launch comment.
3. decide whether the run needs a stop or a reap.
4. only then reach for deeper host inspection.

## See also

- [container-contract.md](container-contract.md) - mounts and env.
- [container-substrate.md](container-substrate.md) - `/substrate` and grants.
