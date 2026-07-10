---
doc_goal: State the config lookup path in a small, durable form so the repo gate and the agent container can point at one shared source of truth.
---
# ward config discovery

Ward resolves config from the repo and the selected launch context.

- `.ward/ward.yaml` drives the repo gate.
- launch-time sources can override edge surfaces.
- the chosen config is always part of the run's audit trail.

## See also

- [ward-yaml.md](ward-yaml.md) - the repo schema.
- [config-source.md](config-source.md) - launch-time sources.

## Practical path

- the repo config is the first thing to check for dev verbs.
- launch-time config comes next when the agent or operator surface needs it.
- the selected source should be obvious enough that a user can explain it
  after the run.
