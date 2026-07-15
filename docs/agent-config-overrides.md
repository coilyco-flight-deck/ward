---
doc_goal: Keep the config-overrides anchor stable after the old page was collapsed.
---
# agent config overrides

This page is the durable anchor for per-role overlay precedence.

- It covers the merge between fleet defaults and role-specific overrides.
- It keeps the embedded fleet config and the launch source model distinct.
- The comment trail points here when a role overlay wins over a base value.
- The shared model override shape is `agent.<harness>.model`, including Goose
  through `--config agent.goose.model=<model>`.

## See also

- [config-source.md](config-source.md) - launch-time source selection.
- [agent-harnesses.md](agent-harnesses.md) - the harness axis.
