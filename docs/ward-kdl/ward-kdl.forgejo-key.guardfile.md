# ward-kdl ops forgejo-key

Exec-dialect CLI. Every verb runs `kubectl` with the granted subcommand (or its `argv` override) appended; the binary and its prefix are fixed and the caller can never substitute them.

## ward-kdl ops forgejo-key read - read ONLY the example forgejo api-token from the k3s external-secrets mirror, decoded

`kubectl get secret example-runner-secrets -n example -o go-template={{index .data "api-token" | base64decode}}`

Flags: only `--ward-sealed-single-key` allowed (strict allowlist).

Preflight:

- denies when any-arg matches *

## See also

- [ward-kdl.md](../ward-kdl.md) - the build-time authoring layer behind this surface
- [ward-kdl-surface.md](../ward-kdl-surface.md) - the full generated verb surface, area by area
