---
doc_goal: Give a brand-explicit, source-aligned matrix of Ward's shipped providers, partial seams, auth sources, and explicit non-providers.
---
# Compatibility surface

## Forges and trackers

* Forgejo - shipped for checkout, issues, pull requests, Actions status, merge,
  backpressure, and canonical release automation.
* GitHub - shipped for checkout and issue-thread control. Forgejo-native PR
  creation, merge/status gates, rerun, and open-PR backpressure are not shipped.
* Shortcut - shipped as an issue tracker.
* GitLab, Trello, Jira, and Linear - not Ward providers.

Tracker, checkout, and landing providers may differ. Ward resolves their typed
adapters independently.

## Containers

* Docker - shipped runtime.
* Podman - not a Ward runtime.

## Harnesses

* Claude, Codex, Goose, and OpenCode - shipped typed harness adapters.
* Aider - not a Ward harness.
* Ollama - supported as a Goose or OpenCode backend input, not as a harness.

See [agent-harnesses.md](agent-harnesses.md) for invocation, model, endpoint,
and host-auth details.

## Auth and config sources

* Forgejo broad API credential - broker process only. Engineer and QA use a
  distinct Git token and role-bound typed broker operations.
* Codex - `~/.codex/auth.json`, with the Codex CLI `Codex Auth` Keychain item
  as the macOS fallback.
* Claude - host subscription login under `~/.claude`.
* GitHub - `WARD_GITHUB_TOKEN_SOURCE=env|gh|app`. Each is an explicit
  user-provided source. App mode requires registered App inputs.
* Shortcut - `SHORTCUT_API_TOKEN`.
* Ward preferences - `~/.ward/config.yaml` and repository `.ward/ward.yaml`.

## Embedded and external behavior

Ward compiles harness adapters, launch defaults, container payloads, workflow
roles, and broker policy as Go values. It does not vendor Docker, Git, or
harness CLIs. Provider-specific operator automation, hosted alert routing, and
fleet convergence are outside Ward.

## See also

* [architecture.md](architecture.md) - ownership and authority boundaries.
* [agent-dispatch-broker.md](agent-dispatch-broker.md) - forge credential boundary.
* [container.md](container.md) - runtime contract.
