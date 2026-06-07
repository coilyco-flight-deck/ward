# Security Policy

Hello and thank you for your interest! :tada: :lock:

## Supported versions

This package is at v0. Only the latest commit on `main` is supported for security fixes. No published releases yet to backport to.

| Version             | Supported          |
| ------------------- | ------------------ |
| `main` (latest)     | :white_check_mark: |
| any pinned commit   | :x: (upgrade)      |

## Reporting a vulnerability

Please disclose any vulnerabilities by emailing [coilysiren@gmail.com](mailto:coilysiren@gmail.com). Expect a first response within 48 hours. This project is run on volunteer time, so please have patience :bow:

## What counts as a vulnerability

ward wraps [cli-guard](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard). Most boundary-level issues belong upstream, not here. Specifically interested in reports of:

- ward verbs that bypass the cli-guard policy gate they claim to install
- audit log entries written by ward that are unparseable, truncatable, or omittable
- `.ward/ward.yaml` parse paths that execute shell or import host state in ways the README does not describe
- on Linux, a descendant of a sandboxed ward verb (e.g. `ward pkg brew`) invoking a wrapped tool — by name or absolute path — without re-entering the gate. ward runs sandboxed verbs inside cli-guard's `sandbox` jail so the wrapper holds at arbitrary process depth, not just depth 0; an escape is a vulnerability. The jail is Linux-only — on macOS/Windows enforcement is depth-0 (the harness allowlist) and descendant bypass is a known limitation, not a vulnerability

Out of scope (file as regular issues, not vulnerabilities):

- bare cli-guard framework bugs, report those at [coilyco-flight-deck/cli-guard](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/issues)
- bare urfave/cli framework bugs, report those at [urfave/cli](https://github.com/urfave/cli/issues)
