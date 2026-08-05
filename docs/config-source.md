---
doc_goal: Explain Ward's supported runtime settings and their ownership.
---
# Ward runtime configuration

Ward launches without an external policy bundle. Harness adapters, topology,
execution limits, and the three workflow labels are typed product code.

Supported preferences have these owners:

- `~/.ward/config.yaml` holds operator defaults such as `default-harness`,
  `agent.image`, `agent.release-channel`, workflow defaults, `agent.redaction`,
  `container.staging-dir`, and director limits.
- `.ward/ward.yaml` holds repository-local `agent.workflow`, `agent.image`, and
  `agent.release-channel` values beside the repository's dev verbs. It also
  owns the explicit `agent.verification.fixtures` admission list for bounded
  live-verification targets.
- Explicit command flags win over YAML for the setting they name.
- Harness-owned environment variables select models, endpoints, reasoning, and
  display identity. Ward does not supply those values through a role profile.

The effective precedence is explicit flag or harness environment, repository
YAML, operator YAML, then Ward's typed default.

Container staging is the host-placement exception: `container.staging-dir`,
then `WARD_STAGING_DIR`, then the platform default. Repository YAML never
participates.

`agent.redaction.env-names` lists additional credential-bearing environment
keys whose exact nonempty values Ward must scrub. `agent.redaction.patterns`
lists additional RE2 patterns. Invalid names or patterns fail `ward doctor` and
dispatch before launch. These settings are operator-local and never belong in
repository `.ward/ward.yaml`. Put environment variable names and patterns in
configuration, never literal credential values.

```yaml
agent:
  redaction:
    env-names:
      - PRIVATE_SERVICE_TOKEN
    patterns:
      - 'tenant-secret-[0-9]+'
```

See [config-migration.md](config-migration.md) for the removed configuration
surfaces and direct replacements.
