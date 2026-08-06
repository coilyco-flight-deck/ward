---
doc_goal: Define Ward's complete supported config discovery, ownership, precedence, fields, and migration contract.
---
# Ward configuration

Ward's harness adapters, roles, topology, authority, and execution limits are
typed product behavior. YAML configures only documented preferences and
repository commands.

## Sources and precedence

* `--config PATH`, then `WARD_CONFIG`, then a cwd walk-up selects the repository
  config. Walk-up prefers `.ward/ward.yaml` and accepts legacy `.coily/coily.yaml`.
* Explicit agent flags or harness-owned environment values win for the setting
  they name.
* Repository `agent` values override operator values in `~/.ward/config.yaml`.
* Operator values override Ward's typed defaults.
* Container staging is separate: `container.staging-dir`, then
  `WARD_STAGING_DIR`, then the platform default.

## Operator fields

`~/.ward/config.yaml` supports:

* `default-harness`.
* `director.default-scope`, `director.max-parallel`, and `director.limit`.
* `agent.image`, `agent.release-channel`, `agent.workflow.default`, and
  `agent.workflow.repositories.<owner/repo>`.
* `agent.review.skip` selectors.
* `agent.redaction.env-names` and `agent.redaction.patterns`.
* `container.memory-limit` and `container.staging-dir`.

Environment names and RE2 patterns may describe secrets. Literal credentials
must never be stored as redaction configuration.

## Repository fields

`.ward/ward.yaml` owns declared commands, security policy, catalog metadata,
and repository-local `agent.workflow`, `agent.image`, and
`agent.release-channel`. See [ward-yaml.md](ward-yaml.md).

## Harness inputs

Models, endpoints, reasoning, and display identity remain harness-owned inputs.
See [agent-harnesses.md](agent-harnesses.md) for exact environment and auth sources.

## Removed config

Ward does not load a runtime policy bundle, fleet-local file, role profiles,
custom startup roles, role capability presets, role-specific network or merge
grants, or a director polling ledger. Move surviving preferences to the fields
above and delete the retired source.

## See also

* [doctor.md](doctor.md) - validation and remedies.
* [container-staging.md](container-staging.md) - host placement exception.
