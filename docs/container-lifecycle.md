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
- Before a stopped engineer container can be removed, Ward preserves any
  unlanded committed Git history as a host-owned rescue artifact under
  `~/.ward/rescues/<run-id>/`. The artifact contains verified Git bundles and a
  manifest, never a workspace or ignored files.

## Forge outage recovery

When a forge failure prevents landing, use `ward agent recover owner/repo#N`.
It prints the newest matching rescue plan without mutation. After reviewing the
file inventory and any quarantine marker, run it again with `--apply --work
<fresh-clean-clone>` to prepare an `issue-N-recovery` branch from current
`origin/main`. That branch then follows the normal PR workflow.

Rescues are intentionally retained across ordinary reaping and broker restarts.
They are Git-object-only: Ward does not copy ignored files, credentials, or
arbitrary container state. Large deletion sets and generated binaries are
quarantined and require `--include-quarantined` on the explicit recovery run.
After a consumed artifact has passed the retention window, remove it only with
`ward agent recover prune --older-than 720h --confirm`.

## Launch details

- the host stages the package's matching Linux ward binary for the entrypoint.
  Linux hosts copy the running executable. macOS and Windows packages carry a
  same-version Linux sidecar. Explicit version pins still use release assets,
  and older packages without a sidecar fall back to that download path.
- per-run launch assets and credential env-files share the platform staging
  root described in [container-staging.md](container-staging.md). On Windows,
  Ward verifies both the env-file DACL and Docker Desktop's ability to read the
  assets bind before handing credentials to the run.
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
