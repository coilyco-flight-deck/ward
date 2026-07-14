# Security Policy

Hello and thank you for your interest! :tada: :lock:

## Supported versions

This package is at v0. Only the latest commit on `main` is supported for security fixes. Published releases are not backported.

| Version             | Supported          |
| ------------------- | ------------------ |
| `main` (latest)     | :white_check_mark: |
| release or commit   | :x: (upgrade)      |

## Reporting a vulnerability

Please disclose any vulnerabilities by emailing [coilysiren@gmail.com](mailto:coilysiren@gmail.com). Expect a first response within 48 hours. This project is run on volunteer time, so please have patience :bow:

## What counts as a vulnerability

ward has two security-relevant surfaces, and a report is welcome against either: the **dev-verb gate** (`ward exec`, wrapping cli-guard) and the **agent/container surface** (`ward agent`, the README's headline - an agent harness in a credential-holding container). Each is scoped below.

### The dev-verb gate (`ward exec`)

ward wraps [cli-guard](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard). Most boundary-level issues belong upstream, not here. Specifically interested in reports of:

- ward verbs that bypass the cli-guard policy gate they claim to install
- audit log entries written by ward that are unparseable, truncatable, or omittable
- `.ward/ward.yaml` parse paths that execute shell or import host state in ways the README does not describe
- on Linux, a descendant of a sandboxed ward verb (e.g. `ward docker exec`) invoking a wrapped tool — by name or absolute path — without re-entering the gate. ward runs sandboxed verbs inside cli-guard's `sandbox` jail so the wrapper holds at arbitrary process depth, not just depth 0; an escape is a vulnerability. The jail is Linux-only — on macOS/Windows enforcement is depth-0 (the harness allowlist) and descendant bypass is a known limitation, not a vulnerability

The gate is **verb-level** - it bounds what call is expressible, not what a process can touch once running. [docs/exec-verb.md](docs/exec-verb.md) is the short reference for what it does and does not defend, and why an escaped process is the container's job, not the gate's.

### The agent/container surface (`ward agent`)

`ward agent` fresh-clones the target into a throwaway container, seeds real host credentials (the harness login, the bot `FORGEJO_TOKEN`, and the opt-in `~/.aws` mount), runs the agent under `bypassPermissions` with no deny wall, and pushes to `main`. The container's isolation is the **sole** boundary ([docs/container.md](docs/container.md)). Interested in reports of:

- credential leakage: a seeded secret (the claude OAuth blob, codex auth, `FORGEJO_TOKEN`, `~/.aws`) reaching argv, the audit log, or an `env` dump - anywhere outside its mode-600 `--env-file` / git-credential file ([docs/agent-lifecycle.md](docs/agent-lifecycle.md))
- container escape: a run reaching the host filesystem past the read-only cwd bind, the docker socket, or another concurrent container. Isolation is the only boundary, so an escape past the one throwaway clone is a vulnerability, not a known limitation
- cross-repo credential bleed: in a multi-repo run (`--repo` grants), one repo's push token or credential being usable against a repo outside the granted set, or a run reaching a repo it was never granted
- telemetry or audit gaps in a `ward agent` run (parallel to the exec audit-log bullet): a drained `meta.json` or log archive leaking a secret the redaction should have dropped, or a run leaving no reconstructable record ([docs/agent-ops.md](docs/agent-ops.md))

Out of scope (file as regular issues, not vulnerabilities):

- bare cli-guard framework bugs, report those at [coilyco-flight-deck/cli-guard](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/issues)
- bare urfave/cli framework bugs, report those at [urfave/cli](https://github.com/urfave/cli/issues)
