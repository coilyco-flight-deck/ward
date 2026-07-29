---
doc_goal: Direct operators from the removed fleet-local surface to supported YAML.
---
# Operator launch preferences

Ward reads operator launch preferences from `~/.ward/config.yaml`. There is no
second fleet-local file and no role-profile merge.

Supported keys include `default-harness`, `agent.image`,
`agent.release-channel`, `agent.workflow`, and director limits. Repository YAML
and explicit inputs take precedence as documented in
[agent-config-overrides.md](agent-config-overrides.md).

See [config-migration.md](config-migration.md) for direct migrations from older
files.
