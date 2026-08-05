---
doc_goal: Map ward's compatibility seams to the real ports and reference adapters so a contributor can extend the right boundary and see what ward embeds instead of vendoring.
---
# Compatibility surface

ward stays small by pushing stack-specific behavior behind a few seams. This page is the release-facing matrix for the external systems ward can talk to today, plus the ones it explicitly does not.

States mean:

- shipped - the adapter or guarded surface is in tree and part of the release
- partial - only part of the provider surface is wired today
- planned or deferred - tracked work, not shipped
- not a ward provider - explicitly out of scope

## Git platforms / forges

- Forgejo - shipped for Ward's native issue and PR control plane. `cmd/ward/forgejo_ops.go`.
- GitHub - shipped for the issue-thread control plane. `cmd/ward/github_ops.go`.
  PR creation, the Forgejo-native PR workflow, PR-status merge gate, rerun,
  and open-PR backpressure remain Forgejo-only until GitHub grows matching
  adapters; the gap is called out in the GitHub contract tests.
- GitLab - not a ward provider. `CONTRIBUTING.md`.

## Issue trackers

- Forgejo, GitHub, Shortcut - shipped. `cmd/ward/forgejo_ops.go`, `cmd/ward/github_ops.go`, `cmd/ward/shortcut_ops.go`.
- Trello - not a Ward tracker provider.
- Jira, Linear - not a ward provider.

## Container runtimes

- Docker - shipped. `cmd/ward/container.go`, `docs/container.md`.
- Podman - not a ward provider. `CONTRIBUTING.md`.

## Agent harnesses

- Claude, Codex, Goose, Opencode - shipped. `docs/agent-harnesses.md`, `docs/agent-claude.md`, `docs/agent-codex.md`, `docs/agent-goose.md`, `docs/agent-opencode.md`.
- Aider - not a Ward harness.
- Ollama - partial backend, not a harness. `docs/agent-harnesses.md`, `docs/agent-goose.md`, `docs/agent-opencode.md`.

## AOS operator providers

- Generated operator leaves belong to `aosguard ops` in AOS. They are not Ward commands.

## Config and auth sources

- Ward launch mechanics are typed product code. AOSguard owns its own
  operator-spec configuration.
- Compose directors use Ward's native sibling broker for authenticated Forgejo
  operations. The broker snapshots the host-resolved token and exposes only
  Ward's allowlisted request shapes, never the credential.
- Codex host login - `~/.codex/auth.json` on every platform, with Codex CLI's
  `Codex Auth` login-keychain item as the macOS fallback.
- `~/.ward/config.yaml` - operator launch preferences.
- `WARD_GITHUB_TOKEN_SOURCE`, `env`, `gh`, `app` - shipped GitHub token path. `cmd/ward/forge.go`, `cmd/ward/github_app.go`.
- `SHORTCUT_API_TOKEN` - shipped operator input. `cmd/ward/shortcut_ops.go`, `cmd/ward/forgejo_ops.go`.

## Adding your stack

- GitHub + Issues - reuse the GitHub forge adapter, keep the current runtime, and only split the tracker seam if needed.
- GitLab + Issues - add a forge adapter for GitLab, then keep or split the tracker seam to match the issue API.
- GitHub + Trello - keep the GitHub forge adapter and add a Trello tracker adapter.
- GitLab + Shortcut - add both the GitLab forge adapter and the Shortcut tracker adapter.
- Any stack + podman - keep the higher-level launch flow and replace the container runtime seam.

## On embedding

Ward compiles its launch defaults and container payloads as Go values in
`cmd/ward/container_payloads.go` and the consuming command code. It does not
ship source-side asset bundles or vendor docker, git, or agent CLIs.

## See also

- [CONTRIBUTING.md](../CONTRIBUTING.md)
- [agentsapi.md](agentsapi.md)
- [container.md](container.md)
