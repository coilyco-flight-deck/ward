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

ward wraps [cli-guard](https://github.com/coilysiren/cli-guard). Most boundary-level issues belong upstream, not here. Specifically interested in reports of:

- ward verbs that bypass the cli-guard policy gate they claim to install
- audit log entries written by ward that are unparseable, truncatable, or omittable
- `.ward/ward.yaml` parse paths that execute shell or import host state in ways the README does not describe

Out of scope (file as regular issues, not vulnerabilities):

- bare cli-guard framework bugs, report those at [coilysiren/cli-guard](https://github.com/coilysiren/cli-guard/issues)
- bare urfave/cli framework bugs, report those at [urfave/cli](https://github.com/urfave/cli/issues)
