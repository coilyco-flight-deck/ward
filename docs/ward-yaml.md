---
doc_goal: Let an author write and validate a `.ward/ward.yaml` from ward docs alone - every `commands:` and `security:` field rendered from the cli-guard repocfg struct - and, since this gates a security tool, be brutally honest about which fields ward actually enforces versus parses-but-no-ops so nothing reads as protected when it is not.
---
# .ward/ward.yaml field reference

The per-repo allowlist ward reads. It lives at `.ward/ward.yaml` (or the
honored legacy `.coily/coily.yaml`), one per repo, resolved by the walk-up in
[config-discovery.md](config-discovery.md).

**Where the schema lives.** The struct ward parses is the cli-guard `repocfg`
format, authored upstream in `cli-guard/cli/repocfg` (`repocfg.go` +
`security.go`) and pinned into ward via `go.mod`. This page is the
field-by-field render of that struct, so a `.ward/ward.yaml` can be authored
from ward docs alone. When ward's pinned cli-guard bumps, this page is the thing
to re-check against `repocfg`.

**Current release note.** The live release surface no longer ships `ward setup`
or `ward doctor`. The former setup/doctor behavior is preserved in
[ward-setup-doctor-inventory.md](ward-setup-doctor-inventory.md)
for the rebirth pass.

**What ward's loader reads.** Only two top-level keys: `commands:` and
`security:`. Any other top-level key (including `catalog:`) is ignored by ward -
see [Top-level keys](#top-level-keys). A key ward reads is not always a key ward
**acts on**: some fields are parsed and validated but not yet wired into a
runtime effect. The [What ward reads, ignores, and no-ops](#what-ward-reads-ignores-and-no-ops)
section is the honest map, because a security field that parses cleanly but
enforces nothing is the worst failure shape for a security tool.

**Validate before you trust it.** Ward parses this file **best-effort**. Parse
or validation failures surface only through the config load path, not as a
runtime security gate. So a `security:` block with a typo does not imply
protection, and the repo owner should treat it as untrusted until the schema is
checked. The retired `ward doctor` loud-validator behavior is preserved in the
release-planning inventory instead of the live surface.

## Top-level keys

- **`commands:`** - map of dev-verb name to its declaration. Read by ward. See [commands](#commands).
- **`security:`** - the security policy block. Parsed by ward for schema compatibility, but not enforced by any live ward surface. The retired `ward doctor` fail-on-missing-block behavior is captured in the release-planning inventory. See [security](#security).
- **`catalog:`** - **not read by ward for repo verbs.** This is `coilyco-flight-deck/agentic-os` catalog tooling metadata (repo description + cross-repo `dependsOn`). ward's `repocfg` loader still unmarshals only `commands` + `security` for config compatibility, while every warded agent role (engineer, director, advisor) reads `catalog.dependsOn` at launch to auto-mount those upstreams as read-only reference clones ([ward#573](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/573); [container-multi-repo.md](container-multi-repo.md)). A `dependsOn` entry may be a bare `owner/name` (resolved on canonical Forgejo over HTTPS) **or a full git clone URL** carrying a non-Forgejo host and transport - `ssh://git@github.com/StrangeLoopGames/Eco.git`, `git@github.com:owner/name.git`, or a bare `github.com/owner/name` (synthesized to the sanctioned ssh form). An external host is honored over its own transport off a **host-side ssh-seeded** gitcache mirror, never mirrored onto Forgejo, and a dep that does not hydrate **fails loud** at launch instead of silently reading as present ([ward#612](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/612)). It is safe to include for the catalog tooling and safe to omit if you do not use it.

## commands

Each entry under `commands:` is one dev verb exposed through `ward exec <name>`
(and, via the [unknown-verb fallback](verb-fallback.md), as bare `ward <name>`).

**The command name** (the map key) is validated: lowercase letters, digits, and
single dashes only, and it may not start or end with `-`. An illegal character
fails the whole load.

Two forms are accepted for the value:

- **Scalar form** - the value is the `run` string directly, with no description: `fmt: make fmt`.
- **Mapping form** - a `{run, description, allow_metacharacters, audit}` object.

Mapping-form fields:

- **`run:`** - **required.** The command line, split on whitespace into an argv slice (`strings.Fields`, not a shell parse). `run: make build` becomes `["make", "build"]`. `argv[0]` is resolved via `$PATH` at exec time. An empty `run` (or one that parses to zero tokens) fails the load. Every token is checked against cli-guard's shell-metacharacter policy at load time (unless `allow_metacharacters` is set), so `run: sh -c 'a && b'` is rejected - `run:` is a single argv, never a shell pipeline.
- **`description:`** - optional. Human blurb shown in `ward exec --help`. It was also one side of the retired `ward doctor` drift check, now captured in the release-planning inventory. Defaults to empty.
- **`allow_metacharacters:`** - optional bool, default `false`. A per-command escape hatch that opts this verb **out** of the shell-metacharacter validator, for both the YAML-declared argv tokens (skipped at load) and the caller-supplied args at exec time. When set, ward stamps the audit row with `policy_skipped=true`. Use only when the command legitimately needs a metacharacter in an argument. It widens the gate for that one verb, so it is loud in the audit trail on purpose.
- **`audit:`** - optional mapping. Its one field is **`egress:`** (bool, default `false`), which in cli-guard opts a command into a per-invocation CONNECT egress proxy that a consumer wires around exec. **ward's `exec` does not currently wire that proxy for repo verbs**, so `audit.egress` on a `commands.<name>` entry is parsed and validated but has no runtime effect in ward yet. Declaring it is harmless and forward-compatible, not active.

### The Makefile contract

`ward exec <name>` runs whatever argv `run:` declares - it does **not** require a
Makefile. `run: go build ./...` or `run: cargo test` works fine, and a repo that
only ever calls `ward exec` needs no Makefile at all.

The former `ward doctor` allowlist contract is now inventory-only. The rebirth
pass will restore a smaller version of the drift guard before release.

### The build/test/install triple

A ward-managed repo is **expected** to declare `build`, `test`, and `install`
so `ward build` / `ward test` / `ward install` work bare (the three verbs a
headless agent needs to bootstrap an unfamiliar repo). This is a **convention,
not a ward-binary check**: `ward exec` requires none of it. Any enforcement of
the triple is a separate fleet-rolled pre-commit linter in
`coilyco-flight-deck/agentic-os`, not the `ward` binary. A toy or non-ward repo
can declare any subset of verbs. See [verb-fallback.md](verb-fallback.md).

## security

The policy block ward parses. A zero value (no `security:` key) means no policy
declared. The retired `ward doctor` inventory captures the former
fail-on-missing-block behavior. Every sub-block below is optional on its own.

### security.protected_binaries[]

A list of host tools a semi-trusted agent must not invoke directly. Parsed for
schema compatibility and inventory, not enforced by ward today.

Per entry:

- **`name:`** - **required.** The binary's bare basename, e.g. `gcloud`. It must be a basename, not a path (`/opt/homebrew/bin/gcloud` is rejected at load). Duplicate names across entries fail the load. The retired hook surface matched by basename, so one declaration covered the bare token, an absolute path, and a relative path to the same binary.
- **`mode:`** - optional. How the binary is protected. Legal values: unset (empty) or `deny-direct`. Empty defaults to `deny-direct`, which is the **only** value v1 supports - any other string (e.g. `warn`) fails the load. Do not invent modes.
- **`allowed_wrappers:`** - optional list of wrapper command names a human routes through instead (e.g. `cloud-ops`). Preserved for inventory compatibility. This list nests **under each protected-binary entry**, not at the top of `security:`.
- **`expected_real_paths:`** - optional list of canonical install locations of the real binary. This was read by the retired `ward doctor` path probe and is preserved here in the inventory only.
- **`credential_env:`** - optional list of environment variable names that hand the agent this binary's credentials when set. This was read by the retired `ward doctor` credentials probe and is preserved here in the inventory only.

### security.sudo

- **`forbid_passwordless:`** - optional bool, default `false`. This was read by the retired `ward doctor` sudo probe and is preserved here in the inventory only.

### security.hooks

The optional deny/route block. ward parses it for compatibility, but does not
apply it in any live surface today.

- **`deny_bare_binaries:`** - optional list of basenames the parser accepts for compatibility, even when they are not listed as `protected_binaries`.
- **`route_hints:`** - optional map of binary basename to the recovery sentence surfaced when its invocation is denied. Takes precedence over a hint synthesized from `allowed_wrappers`. Example: `route_hints: {kubectl: "route kubectl through cloud-ops"}`.

### security.forbidden_argv[]

Glob-pattern argv deny rules that match a whole command segment (not just a
basename), for denying, e.g., write verbs or destructive flags.

Per entry:

- **`description:`** - **required.** Human label surfaced in the deny hint. An empty description fails the load.
- **`matches_glob_any:`** - **required, non-empty.** A list of POSIX glob patterns (Go `path/filepath.Match` grammar). A command segment matching **any** pattern is denied. Each pattern is validated at load - an empty or malformed glob fails the load.
- **`hint:`** - optional recovery sentence. When empty, the engine synthesizes one from the matched glob.

> **Caveat - not enforced by ward today.** cli-guard's `repocfg` fully parses and
> validates `forbidden_argv`, and cli-guard exposes the mapping (`hookcfg.ForbiddenFor`).
> Ward does not wire that mapping into any live surface, so a `forbidden_argv`
> block declared in a ward repo is validated but **enforces nothing at runtime yet**.
> Declaring it is safe and forward-compatible, but do not treat it as an active
> gate in ward today.

## A complete annotated example

```yaml
# .ward/ward.yaml

# catalog: is NOT read by ward - it is agentic-os catalog tooling metadata.
# ward's loader reads only `commands:` and `security:`. Safe to include or omit.
catalog:
  description: "sample-tool - a demo repo."
  dependsOn:
    - forgejo.example.org/org/some-dependency        # Forgejo: HTTPS-token gitcache path
    - ssh://git@github.com/StrangeLoopGames/Eco.git  # external: honored over ssh, seeded host-side (ward#612)

commands:
  # Mapping form: `run` required, the rest optional.
  build:
    run: make build              # split on whitespace into ["make", "build"]
    description: Build all packages.
  test:
    run: make test
    description: Run the unit test suite.
  install:
    run: make install
    description: Install the binary.

  # Scalar form: the value IS the run string, no description.
  fmt: make fmt

  # Escape hatch + audit hint (both default false; shown here for illustration).
  cover:
    run: make cover
    description: Unit tests with a coverage profile.
    allow_metacharacters: false  # true would skip the metacharacter validator
    audit:
      egress: false              # parsed, but not wired for repo verbs in ward yet

security:
  protected_binaries:
    - name: gcloud               # bare basename, required
      mode: deny-direct          # optional; deny-direct is the only v1 value
      allowed_wrappers:          # nests under THIS entry, not under security:
        - cloud-ops
      expected_real_paths:
        - /opt/homebrew/bin/gcloud
      credential_env:
        - CLOUDSDK_AUTH_ACCESS_TOKEN
  sudo:
    forbid_passwordless: true    # doctor FAILs if `sudo -n true` succeeds
  hooks:
    deny_bare_binaries:
      - kubectl
    route_hints:
      kubectl: "route kubectl through cloud-ops"
  # forbidden_argv is parsed + validated but not enforced by ward today:
  forbidden_argv:
    - description: "deny destructive terraform"
      matches_glob_any:
        - "terraform apply*"
        - "terraform destroy*"
      hint: "run plans only - applies go through CI"
```

Validate the live parts with `ward exec`. The retired `ward doctor`
inventory documents the drift check and probe summary that used to live here.

## What ward reads, ignores, and no-ops

The honest map, so nothing reads as protected when it is not:

- **Consumed by ward (has runtime effect)** - `commands.<name>.run`, `commands.<name>.description`, `commands.<name>.allow_metacharacters`.
- **Inventory only** - `commands.<name>.audit.egress`, `security.protected_binaries[]`, `security.protected_binaries[].mode`, `security.protected_binaries[].allowed_wrappers`, `security.protected_binaries[].expected_real_paths`, `security.protected_binaries[].credential_env`, `security.sudo.forbid_passwordless`, `security.hooks.*`, `security.forbidden_argv[]`. These fields are preserved in the inventory doc because the retired `ward doctor` surface used them, but they do not gate anything in the live surface.
- **Ignored by ward entirely** - `catalog:` for repo verbs and any other unrecognized top-level key. Read by external agentic-os tooling, and by every warded agent role at launch for its repo-local `dependsOn` read-only context list ([ward#573](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/573)), never by ward's exec surface.

## See also

- [config-discovery.md](config-discovery.md) - how ward resolves which `.ward/ward.yaml` to read.
- [exec-verb.md](exec-verb.md) - `ward exec <verb>`, the dev-verb gate that runs `run:`.
- [verb-fallback.md](verb-fallback.md) - bare `ward <verb>` fallback + the build/test/install triple.
- [ward-setup-doctor-inventory.md](ward-setup-doctor-inventory.md) - the retired setup/doctor inventory.
- [../.ward/ward.yaml](../.ward/ward.yaml) - ward's own live config.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
