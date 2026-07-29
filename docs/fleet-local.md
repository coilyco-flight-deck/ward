---
doc_goal: Direct operators from the removed fleet-local surface to supported YAML.
---
# Operator launch preferences

Ward reads operator launch preferences from `~/.ward/config.yaml`. There is no
second fleet-local file and no role-profile merge.

Supported keys include `default-harness`, `agent.image`,
`agent.release-channel`, `agent.workflow`, `container.staging-dir`, and
director limits. Container staging is host placement: the operator value wins
over `WARD_STAGING_DIR`, then Ward uses the platform default. Repository YAML
and explicit agent inputs take precedence for agent settings as documented in
[agent-config-overrides.md](agent-config-overrides.md).

See [config-migration.md](config-migration.md) for direct migrations from older
files.
