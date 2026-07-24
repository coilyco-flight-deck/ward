---
doc_goal: Define Ward's authority-only consumer contract for agent-compose bundles in ephemeral containers.
---
# agent-compose container handoff

Ward can launch any agent surface with one already-materialized agent-compose
bundle:

```bash
warded engineer owner/repo#123 --agent-compose-bundle /path/to/bundle
```

The host resolves the path and requires a readable regular `manifest.json`
before Docker starts. Ward then mounts the directory read-only at
`/opt/agent-compose-bundle`. The container bootstrap runs these provider-owned
operations before the harness starts:

```text
agent-compose verify /opt/agent-compose-bundle
agent-compose project /opt/agent-compose-bundle --layout <harness> --scope home --target "$WARD_AGENT_HOME"
```

A missing path, unreadable manifest, failed bundle verification, unsupported
harness layout, foreign projection target, or projection error stops the
launch. A launch without `--agent-compose-bundle` keeps the existing behavior
and does not invoke agent-compose.

On `warded director`, the bundle belongs to the director's own surface and is
not forwarded to engineers. Role-specific child bundles must be supplied on
their own host launches. Ward refuses a bundle-backed nested dispatch because
Docker cannot preserve the parent container's host source as a read-only bind.

## Ownership boundary

* Ward owns the flag, host-path validation, Docker mount, selected harness,
  container HOME, launch failure policy, credentials, permissions, network,
  filesystem authority, and teardown.
* Agent-compose owns bundle schema validation, identity selection, skill
  materialization, load-point layout, transactional projection, and rollback.
* Ward treats bundle contents as opaque. It checks only that `manifest.json`
  is a readable regular file, then calls the agent-compose CLI.
* The bundle grants no command, credential, network, permission, or writable
  filesystem capability.

Ward keeps its authority document outside the immutable bundle. After the
provider projects the selected identity, Ward composes that authority document
into the projected instruction load point. The launched harness therefore sees
both the selected role identity and the run's mechanically enforced authority
boundary.

This is the Ward consumer side of
[agent-compose#17](https://forgejo.coilysiren.me/coilyco-flight-deck/agent-compose/issues/17).

## See also

* [agent.md](agent.md) - the agent entrypoint.
* [container-contract.md](container-contract.md) - mounts, env, and permissions.
* [container-lifecycle.md](container-lifecycle.md) - launch through teardown.
