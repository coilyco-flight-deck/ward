---
doc_goal: Give operators a direct migration from Ward's removed runtime bundle and role profiles.
---
# Runtime configuration migration

Ward no longer loads a runtime policy bundle or role profiles. Unconfigured
launches use typed adapters and fixed workflow behavior.

## Direct replacements

- Move the default harness to `default-harness` in `~/.ward/config.yaml`.
- Move the default image and release channel to `agent.image` and
  `agent.release-channel` in `~/.ward/config.yaml`.
- Move an operator workflow default to `agent.workflow.default`.
- Move per-repository operator workflows to
  `agent.workflow.repositories.<owner/name>`.
- Put a repository-specific workflow, image, or release channel under `agent`
  in that repository's `.ward/ward.yaml`.
- Pass model, endpoint, reasoning, and display identity through the selected
  harness's explicit environment or command configuration.

Example operator config:

```yaml
default-harness: codex
agent:
  image: registry.example/ward
  release-channel: release
  workflow:
    default: merge-remote-main
    repositories:
      owner/repo: pull-request
director:
  max-parallel: 4
  limit: 50
```

Example repository config:

```yaml
commands:
  build:
    run: make build
agent:
  workflow: pull-request
  image: registry.example/ward
  release-channel: candidate
```

## Removed without replacement

Custom startup roles, role capability presets, role-specific model and identity
profiles, role-based network selection, and role-based broker or merge grants
are removed. Operators must not recreate them in YAML. The three fixed workflow
labels are `engineer`, `director`, and `qa`.

Director polling and repository burndown filters are also removed without
replacement. A harness-native goal owns repeated queue reads and dispatch
decisions. `director.limit` remains the live snapshot page size, while
`director.max-parallel` remains a dispatch-health backpressure threshold.

Legacy config-reference environment values are ignored. Operators can delete
them after all hosts run the release containing this migration.
