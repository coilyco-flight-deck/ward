---
doc_goal: Explain Ward's supported runtime settings and their ownership.
---
# Ward runtime configuration

Ward launches without an external policy bundle. Harness adapters, topology,
execution limits, and the three workflow labels are typed product code.

Supported preferences have these owners:

- `~/.ward/config.yaml` holds operator defaults such as `default-harness`,
  `agent.image`, `agent.release-channel`, workflow defaults, and director limits.
- `.ward/ward.yaml` holds repository-local `agent.workflow`, `agent.image`, and
  `agent.release-channel` values beside the repository's dev verbs.
- Explicit command flags win over YAML for the setting they name.
- Harness-owned environment variables select models, endpoints, reasoning, and
  display identity. Ward does not supply those values through a role profile.

The effective precedence is explicit flag or harness environment, repository
YAML, operator YAML, then Ward's typed default.

See [config-migration.md](config-migration.md) for the removed configuration
surfaces and direct replacements.
