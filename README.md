---
doc_goal: Let a new adopter understand, install, preview, and navigate Ward without reading implementation history.
---
# Ward

Ward is a governed execution layer for coding agents and repository commands.
It runs agent work in fresh least-access containers, exposes fixed issue, Git,
pull-request, broker, log, and recovery primitives, and records durable audit
evidence. A harness-native goal chooses and repeats work. Ward does not run an
autonomous backlog scheduler.

Use Ward when work is separable, concurrent, failure-prone, or needs a durable
issue-to-landing trail. A single goal agent may be simpler for one coherent
refactor through a tightly coupled subsystem.

## Product surfaces

* `ward exec <verb>` runs a repository-declared command through argv and
  repository-state checks.
* `ward git ...` exposes governed version-control operations.
* `ward audit ...` reads the append-only JSONL trail.
* `ward agent ...`, also installed as `warded`, launches and operates isolated
  agent runs.

Roles select workflow behavior, harnesses select agent CLI mechanics, and
workflows select landing evidence. None of those labels grants credentials,
mounts, network, broker operations, or merge authority.

## Requirements

The repository command path needs only Ward and a `.ward/ward.yaml`. Agent
execution additionally needs Docker, a supported harness, its host auth or
model endpoint, and tracker credentials for any issue or PR mutation.

Supported hosts are macOS, Linux, and Windows. See the
[compatibility matrix](docs/compat-surface.md) for exact provider support.

## Install

Homebrew on macOS or Linux:

```bash
brew tap coilyco-flight-deck/tap https://forgejo.coilysiren.me/coilyco-flight-deck/homebrew-tap
brew install coilyco-flight-deck/tap/ward
```

Scoop on Windows:

```powershell
scoop bucket add coilyco-flight-deck https://forgejo.coilysiren.me/coilyco-flight-deck/scoop-bucket
scoop install ward
```

Source contributors use the [workspace contract](docs/workspace.md). Each
release publishes a checksummed platform matrix. Forgejo is canonical and the
GitHub release is a verified mirror.

## First commands

```bash
just test
ward git status
ward audit tail --follow
ward setup
ward doctor
warded engineer owner/repo#123 --print
```

`--print` resolves and validates an agent launch without starting a container.
Use a fully qualified `owner/repo#N` in shells so `#` cannot begin a comment.
The [first-run guide](docs/first-run.md) gives the complete safe sequence.

## Execution model

Repository commands use the `umbra` engine for declaration, validation,
routing, and audit. Agent execution adds typed harness adapters, fixed roles
and workflows, reservations, a supervised broker, ephemeral containers,
secret-safe artifacts, and verified teardown. See
[architecture](docs/architecture.md) and [terminology](docs/terminology.md).

Every platform enforces the declared boundary at command entry. Containerized
agent runs use the same least-access contract on every supported host, and the
container is what bounds a process once it is running.

## When a run breaks

Start with [troubleshooting](docs/troubleshooting.md). It maps the observed
symptom to dispatch status, list state, secret-safe logs, issue-thread evidence,
or repository audit, then names the supported remedy.

## Status and support

Ward is pre-1.0 and in active use. Minor compatibility changes can land before
1.0, so downstream automation may pin a release. File public bugs and feature
requests on the [GitHub mirror](https://github.com/coilyco-flight-deck/ward/issues/new).
Canonical development and release automation run on
[Forgejo](https://forgejo.coilysiren.me/coilyco-flight-deck/ward).

## See also

* [documentation index](docs/README.md) - every durable product contract.
* [capabilities](docs/FEATURES.md) - major shipped inventory.
* [agent rules](AGENTS.md) - repository operating doctrine.
* [justfile](justfile) - this repo's own dev verbs.
* [repository config](.ward/ward.yaml) - catalog metadata, and the schema an adopter declares verbs in.
* [repository schema](docs/ward-yaml.md) - complete `.ward/ward.yaml` reference.
