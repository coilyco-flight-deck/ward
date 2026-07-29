---
doc_goal: Keep the repo config schema readable after the docs collapse so adopters can still author `.ward/ward.yaml` from the public docs alone.
---
# `.ward/ward.yaml`

This file tells ward which repo verbs are allowed and how the gate behaves.

## Fields

- `commands` - the allowed repo verbs.
- `security` - optional policy settings for stricter runs.
- `agent.workflow` - optional repository workflow override.
- `agent.image` - optional repository container image override.
- `agent.release-channel` - optional repository image tag override.
- `agent.verification.fixtures[]` - optional bounded live-verification
  admission rules. Each entry requires `repository` in `owner/name` form and
  an `issue-label`.

## Typical shape

```yaml
commands:
  build: make build
  test: make test
  lint: make lint
security:
  allow:
    - git status
agent:
  workflow: pull-request
  image: registry.example/ward
  release-channel: release
  verification:
    fixtures:
      - repository: example/ward-qa-fixture
        issue-label: qa-fixture
```

The real file can carry more verbs, but the shape stays simple: a command map
plus optional security and agent launch preferences.

## Rules

- The file lives at the repo root under `.ward/ward.yaml`.
- It is the whole contract for `ward exec` adoption.
- Agent values override operator YAML and are overridden by explicit command
  inputs.
- Role profiles and authority settings are not valid repository configuration.
- Verification fixture admission is repository configuration, not a role
  authority grant. Ward requires both the configured repository and issue
  label when `--verification-fixture` is present.
- It does not replace the agent container or AOSguard operator surface.

## See also

- [exec-verb.md](exec-verb.md) - how the gate uses the file.
- [config-discovery.md](config-discovery.md) - how ward finds it.
- [config-source.md](config-source.md) - runtime preference ownership.
- [../.ward/README.md](../.ward/README.md) - repository config pointer.
