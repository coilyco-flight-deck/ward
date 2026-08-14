---
doc_goal: Let an adopter author the complete supported `.ward/ward.yaml` schema from this page alone.
---
# `.ward/ward.yaml`

Ward uses this file for commands, repository security, catalog metadata, and launch preferences.

## Commands

`commands` maps string verb names to either a command string or an object:
```yaml
commands:
  build: go build ./...
  test:
    run: go test ./...
    description: Run unit tests.
    allow_metacharacters: false
    audit:
      egress: false
```

`run` and `description` are strings. `run` is required for object form, split into
argv, and validated. Both booleans default false. `description` defaults empty.

## Security

```yaml
security:
  protected_binaries:
    - name: docker
      mode: deny-direct
      allowed_wrappers: [ward]
      expected_real_paths: [/usr/local/bin/docker]
      credential_env: [DOCKER_TOKEN]
  sudo:
    forbid_passwordless: true
  hooks:
    deny_bare_binaries: [docker]
    route_hints:
      docker: Use the declared Ward command.
  forbidden_argv:
    - description: deny destructive Git
      matches_glob_any: ["git reset --hard*"]
      hint: Use a recoverable Git operation.
```

Names, modes, descriptions, hints, and paths are strings. Plural fields are lists
of strings, `route_hints` is a string map, and `forbid_passwordless` is boolean.
An omitted mode defaults to `deny-direct`. Globs match the whole command segment.

## Catalog and agent

```yaml
catalog:
  description: Short repository description.
  dependsOn: [forge.example/owner/repository]
agent:
  workflow: pull-request
  image: registry.example/ward
  release-channel: release
```

`catalog.description` and all `agent` values are strings. `dependsOn` is a list of
repository refs and supplies read-only context repositories. `agent` overrides
operator defaults. Roles, credentials, permissions, networks, broker grants, and
merge authority are not repository-configurable. Omitted sections are empty.
Omitted agent values use operator settings, then compiled defaults shown above.

## Validation

YAML type errors, invalid command argv, duplicate protected binaries,
unsupported modes, invalid globs, and invalid workflows fail loading. Unknown
fields consumed by neither Ward nor umbra have no effect and should be
removed. Run `ward doctor`, then exercise a verb with `ward exec <verb>`.

## See also

* [exec-verb.md](exec-verb.md) - execution contract.
* [config-source.md](config-source.md) - discovery and precedence.
* [../.ward/README.md](../.ward/README.md) - repository config pointer.
