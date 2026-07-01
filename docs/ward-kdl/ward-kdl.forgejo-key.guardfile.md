# ward-kdl ops forgejo-key

Exec-dialect CLI. Every verb runs `kubectl` with the granted subcommand (or its `argv` override) appended; the binary and its prefix are fixed and the caller can never substitute them.

## ward-kdl ops forgejo-key read - read ONLY the forgejo api-token from the k3s external-secrets mirror, decoded

`kubectl get secret forgejo-runner-secrets -n forgejo -o go-template={{index .data "api-token" | base64decode}}`

Flags: only `--ward-sealed-single-key` allowed (strict allowlist).

Preflight:

- denies when any-arg matches *
