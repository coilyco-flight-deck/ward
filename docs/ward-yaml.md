---
doc_goal: Keep the repo config schema readable after the docs collapse so adopters can still author `.ward/ward.yaml` from the public docs alone.
---
# `.ward/ward.yaml`

This file tells ward which repo verbs are allowed and how the gate behaves.

## Fields

- `commands` - the allowed repo verbs.
- `security` - optional policy settings for stricter runs.

## Typical shape

```yaml
commands:
  build: make build
  test: make test
  lint: make lint
security:
  allow:
    - git status
```

The real file can carry more verbs, but the shape stays simple: a command map
plus optional security policy.

## Rules

- The file lives at the repo root under `.ward/ward.yaml`.
- It is the whole contract for `ward exec` adoption.
- It does not replace the agent container or ward-kdl surfaces.

## See also

- [exec-verb.md](exec-verb.md) - how the gate uses the file.
- [config-discovery.md](config-discovery.md) - how ward finds it.
