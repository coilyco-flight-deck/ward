---
doc_goal: Define container launch, secret-safe drain, landed-state proof, rescue, recovery, reaping, retention, and cleanup.
---
# Container lifecycle

1. The host stages a same-version Linux Ward binary, entrypoint, context, and
   secured credential env file.
2. The container clones the target into its writable workspace and starts the
   selected harness.
3. Ward records live console and lifecycle state while the workflow runs.
4. Exit drains secret-safe artifacts and rechecks required landing evidence.
5. Teardown preserves recoverable committed Git state, then removes or retains
   the container according to cleanup status and retention policy.

## Rescue and recovery

Before removing a stopped engineer with unlanded commits, Ward writes verified
Git bundles plus a manifest under `~/.ward/rescues/<run-id>/`. It never copies
the workspace, ignored files, credentials, or arbitrary container state.

`ward agent recover owner/repo#N` previews the newest matching rescue. With
`--apply --work <fresh-clean-clone>`, it prepares an issue recovery branch from
current remote main. Large deletion sets and generated binaries remain
quarantined unless explicitly included. Confirmed pruning removes only
consumed artifacts past the selected retention window.

## Reap and cleanup

Targeted `stop` halts one run. `reap` is the idle-policy backstop. Cleanup
retains failed drains and ambiguous recovery state rather than claiming
success. Ordinary sweeps reclaim stopped Ward containers after the configured
retention TTL, 48 hours by default.

Teardown never treats process exit as landing. It checks current remote main,
pull-request, or remote-branch evidence for the selected workflow. A closing
reference already present on main prevents stale salvage logic from reopening
completed work.

## Debug order

Read dispatch status, secret-safe logs, list state, and the issue workflow
record before stopping or reaping. Never inspect credential-bearing transient
harness state as a substitute for the supported artifacts.

## See also

* [agent-observability.md](agent-observability.md) - drain artifacts.
* [agent-ops.md](agent-ops.md) - stop, reap, logs, and recover.
* [container-staging.md](container-staging.md) - launch assets.
