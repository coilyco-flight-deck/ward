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

**What ward's loader reads.** Only two top-level keys: `commands:` and
`security:`. Any other top-level key (including `catalog:`) is ignored by ward -
see [Top-level keys](#top-level-keys). A key ward reads is not always a key ward
**acts on**: some fields are parsed and validated but not yet wired into a
runtime effect. The [What ward reads, ignores, and no-ops](#what-ward-reads-ignores-and-no-ops)
section is the honest map, because a security field that parses cleanly but
enforces nothing is the worst failure shape for a security tool.

**Validate before you trust it.** The PreToolUse hook (`ward hook`) loads this
file **best-effort**: any parse or validation failure passes through silently
(same posture as a malformed hook payload, see [hook.md](hook.md)). So a
`security:` block with a typo does not error at hook time - it silently enforces
nothing, and the repo owner believes they are protected. **`ward doctor` is the
loud validator**: it surfaces a load failure as a hard error and summarizes the
parsed `security:` block. Run `ward doctor` after every edit to this file. Do
not rely on the hook to tell you the config is wrong.

## Top-level keys

- **`commands:`** - map of dev-verb name to its declaration. Read by ward. See [commands](#commands).
- **`security:`** - the security policy block. Read by ward (doctor + hook). The loader and the hook tolerate its absence, but as of [ward#450](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/450) `ward doctor` **fails** when it is missing (it prints `no security: declared` and exits non-zero), so a real repo declares one. See [security](#security).
- **`catalog:`** - **not read by ward.** This is `coilyco-flight-deck/agentic-os` catalog tooling metadata (repo description + cross-repo `dependsOn`). ward's `repocfg` loader unmarshals only `commands` + `security` and drops everything else, so `catalog:` has zero effect on `ward exec`, `ward doctor`, or the hook. It is safe to include for the catalog tooling and safe to omit if you do not use it.

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
- **`description:`** - optional. Human blurb shown in `ward exec --help`. Also one side of the `ward doctor` drift check (see [the Makefile contract](#the-makefile-contract)). Defaults to empty.
- **`allow_metacharacters:`** - optional bool, default `false`. A per-command escape hatch that opts this verb **out** of the shell-metacharacter validator, for both the YAML-declared argv tokens (skipped at load) and the caller-supplied args at exec time. When set, ward stamps the audit row with `policy_skipped=true`. Use only when the command legitimately needs a metacharacter in an argument. It widens the gate for that one verb, so it is loud in the audit trail on purpose.
- **`audit:`** - optional mapping. Its one field is **`egress:`** (bool, default `false`), which in cli-guard opts a command into a per-invocation CONNECT egress proxy that a consumer wires around exec. **ward's `exec` does not currently wire that proxy for repo verbs** (ward's egress capture today is the separate `ward pkg brew` path), so `audit.egress` on a `commands.<name>` entry is parsed and validated but has no runtime effect in ward yet. Declaring it is harmless and forward-compatible, not active.

### The Makefile contract

`ward exec <name>` runs whatever argv `run:` declares - it does **not** require a
Makefile. `run: go build ./...` or `run: cargo test` works fine, and a repo that
only ever calls `ward exec` needs no Makefile at all.

The Makefile requirement comes from **`ward doctor`**, not from `ward exec`, and
**not** from a `ward lint` verb (there is no `ward lint` - `lint` is only the dev
verb `ward exec lint`, which runs `make lint`). `ward doctor` runs an allowlist
**drift guard** that cross-checks each `commands.<name>` against the repo's
`Makefile`. For a command to pass that check, all three must hold:

- **`run:` is exactly `make <name>`.** `run: bash build.sh` under a `build:` key is a drift failure even though the script runs.
- **The Makefile has a `<name>:` rule carrying a `## <description>` help comment** (the self-documenting-Makefile convention, e.g. `build: ## Build all packages.`). A bare rule with only a recipe and no `## ...` comment is **not** a registered target, so doctor reports it unmatched even though `make <name>` runs by hand.
- **That `## <description>` text equals the command's `description:`** in the yaml, trimmed. Any wording difference is reported as description drift.

So a Makefile is a hard requirement to pass `ward doctor`, but not to use
`ward exec`. Full detail and the minimal passing Makefile are in
[doctor.md](doctor.md) (Allowlist contract).

### The build/test/install triple

A ward-managed repo is **expected** to declare `build`, `test`, and `install`
so `ward build` / `ward test` / `ward install` work bare (the three verbs a
headless agent needs to bootstrap an unfamiliar repo). This is a **convention,
not a ward-binary check**: neither `ward exec` nor `ward doctor` requires the
triple to exist - doctor only validates the commands you actually declare. Any
enforcement of the triple is a separate fleet-rolled pre-commit linter in
`coilyco-flight-deck/agentic-os`, not the `ward` binary. A toy or non-ward repo
can declare any subset of verbs and still pass `ward doctor`. See
[verb-fallback.md](verb-fallback.md).

## security

The policy block that `ward doctor` and `ward hook` read. A zero value (no
`security:` key) means no policy declared. The loader and the hook accept that,
but as of [ward#450](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/450) `ward doctor` **fails** on a missing block (see
[doctor.md](doctor.md)), so a real repo declares one. Every sub-block below is
optional on its own.

### security.protected_binaries[]

A list of host tools a semi-trusted agent must not invoke directly. Fed into the
PreToolUse hook (via cli-guard's `hookcfg.ProtectedFor`) so a bare invocation is
denied with a routing hint, and probed by `ward doctor`.

Per entry:

- **`name:`** - **required.** The binary's bare basename, e.g. `gcloud`. It must be a basename, not a path (`/opt/homebrew/bin/gcloud` is rejected at load). Duplicate names across entries fail the load. The hook matches by basename, so one declaration covers the bare token, an absolute path, and a relative path to the same binary.
- **`mode:`** - optional. How the binary is protected. Legal values: unset (empty) or `deny-direct`. Empty defaults to `deny-direct`, which is the **only** value v1 supports - any other string (e.g. `warn`) fails the load. Do not invent modes.
- **`allowed_wrappers:`** - optional list of wrapper command names a human routes through instead (e.g. `cloud-ops`). Surfaced in the deny hint when no explicit `hooks.route_hints` entry overrides it. This list nests **under each protected-binary entry**, not at the top of `security:`.
- **`expected_real_paths:`** - optional list of canonical install locations of the real binary. `ward doctor`'s `path` probe resolves `name` via `$PATH`. When this list is non-empty, a resolved location outside it is a `FAIL`. When empty, the resolved location is reported as `INFO`. A missing binary is a `WARN`.
- **`credential_env:`** - optional list of environment variable names that hand the agent this binary's credentials when set. `ward doctor`'s `credentials` probe reports each name that is set in the session as a `WARN` (promoted to `FAIL` under `--strict-credentials`).

### security.sudo

- **`forbid_passwordless:`** - optional bool, default `false`. Asserts the agent user must not have broad passwordless sudo. When set, `ward doctor`'s `sudo` probe runs `sudo -n true`: a clean exit is a `FAIL` (passwordless sudo is available), a "password required" non-zero is a `PASS`, any other non-zero is a `WARN`. When unset, the probe is skipped.

### security.hooks

The optional PreToolUse deny/route block. When omitted, protected-binary denial
still derives from `protected_binaries`. This block states extra denials and
hints explicitly.

- **`deny_bare_binaries:`** - optional list of basenames the hook denies on bare invocation, even when they are not listed as `protected_binaries`. Merged with `protected_binaries` into the hook's deny set.
- **`route_hints:`** - optional map of binary basename to the recovery sentence surfaced when its invocation is denied. Takes precedence over a hint synthesized from `allowed_wrappers`. Example: `route_hints: {kubectl: "route kubectl through cloud-ops"}`.

### security.forbidden_argv[]

Glob-pattern argv deny rules that match a whole command segment (not just a
basename), for denying, e.g., write verbs or destructive flags.

Per entry:

- **`description:`** - **required.** Human label surfaced in the deny hint. An empty description fails the load.
- **`matches_glob_any:`** - **required, non-empty.** A list of POSIX glob patterns (Go `path/filepath.Match` grammar). A command segment matching **any** pattern is denied. Each pattern is validated at load - an empty or malformed glob fails the load.
- **`hint:`** - optional recovery sentence. When empty, the engine synthesizes one from the matched glob.

> **Caveat - not enforced by ward today.** cli-guard's `repocfg` fully parses and
> validates `forbidden_argv` (a bad entry fails `ward doctor`), and cli-guard
> exposes the mapping (`hookcfg.ForbiddenFor`). But ward's PreToolUse hook
> currently feeds only `protected_binaries` + `hooks.deny_bare_binaries` into the
> engine - it does **not** call `ForbiddenFor` - so a `forbidden_argv` block
> declared in a ward repo is validated but **enforces nothing at runtime yet**,
> and `ward doctor`'s security summary does not report it. Declaring it is safe
> and forward-compatible, but do not treat it as an active gate in ward today.
> Wiring it into ward's hook is tracked as a follow-up.

## A complete annotated example

```yaml
# .ward/ward.yaml

# catalog: is NOT read by ward - it is agentic-os catalog tooling metadata.
# ward's loader reads only `commands:` and `security:`. Safe to include or omit.
catalog:
  description: "sample-tool - a demo repo."
  dependsOn:
    - forgejo.example.org/org/some-dependency

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
  # forbidden_argv is parsed + validated but NOT enforced by ward's hook yet:
  forbidden_argv:
    - description: "deny destructive terraform"
      matches_glob_any:
        - "terraform apply*"
        - "terraform destroy*"
      hint: "run plans only - applies go through CI"
```

Validate it with `ward doctor` (which also checks the `commands` <-> `Makefile`
drift and summarizes the `security:` block). The hook alone will not tell you if
it is malformed.

## What ward reads, ignores, and no-ops

The honest map, so nothing reads as protected when it is not:

- **Consumed by ward (has runtime effect)** - `commands.<name>.run`, `commands.<name>.description`, `commands.<name>.allow_metacharacters`, `security.protected_binaries[]` (`name` + `allowed_wrappers` in the hook, `expected_real_paths` + `credential_env` in doctor probes), `security.protected_binaries[].mode` (validated), `security.sudo.forbid_passwordless` (doctor probe), `security.hooks.deny_bare_binaries`, `security.hooks.route_hints`.
- **Parsed and validated, but no runtime effect in ward yet** - `commands.<name>.audit.egress` (no egress proxy is wired around repo verbs), `security.forbidden_argv[]` (ward's hook never calls `ForbiddenFor`). Both fail `ward doctor` loudly if malformed, but neither gates anything at runtime today.
- **Ignored by ward entirely** - `catalog:` and any other unrecognized top-level key. Read by external agentic-os tooling, never by ward.

## See also

- [config-discovery.md](config-discovery.md) - how ward resolves which `.ward/ward.yaml` to read.
- [exec-verb.md](exec-verb.md) - `ward exec <verb>`, the dev-verb gate that runs `run:`.
- [verb-fallback.md](verb-fallback.md) - bare `ward <verb>` fallback + the build/test/install triple.
- [doctor.md](doctor.md) - `ward doctor`, the loud validator: allowlist drift + security probes.
- [hook.md](hook.md) - `ward hook`, the PreToolUse deny surface (best-effort, silent on parse failure).
- [../.ward/ward.yaml](../.ward/ward.yaml) - ward's own live config.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
