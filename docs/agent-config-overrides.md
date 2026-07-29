---
doc_goal: Keep the runtime override precedence explicit and testable.
---
# Agent configuration overrides

Ward resolves supported launch settings in this order:

1. An explicit command flag or harness-owned environment variable.
2. Repository values under `agent` in `.ward/ward.yaml`.
3. Operator values in `~/.ward/config.yaml`.
4. Ward's typed product default.

Role labels never participate in this merge. Changing `WARD_ROLE` cannot alter
the model, identity, credentials, mounts, network, broker grants, merge
authority, or container topology.

See [config-source.md](config-source.md) for field ownership and
[agent-harnesses.md](agent-harnesses.md) for harness-specific environment.
