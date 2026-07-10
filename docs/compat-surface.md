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

- Forgejo - shipped. `cmd/ward/forgejo_ops.go`, `docs/ops-forgejo-in-ward.md`, `docs/forgejo-token-audit.md`.
- GitHub - shipped. `cmd/ward/github_ops.go`, `docs/agent-github.md`, `docs/github-token.md`.
- GitLab - not a ward provider. `CONTRIBUTING.md`.

## Issue trackers

- Forgejo issues - shipped. `cmd/ward/forgejo_ops.go`, `docs/ops-forgejo-in-ward.md`.
- GitHub issues - shipped. `docs/agent-github.md`.
- Shortcut Stories - shipped. `docs/shortcut-tracker.md`.
- Trello - not a ward tracker provider. It is a ward-kdl ops surface, not the `Tracker` port. `docs/ward-kdl-surface.md`, `docs/ward-kdl/ward-kdl.trello.guardfile.md`.
- Jira - not a ward provider.
- Linear - not a ward provider.

## Container runtimes

- Docker - shipped. `cmd/ward/container.go`, `docs/container.md`, `docs/container-image.md`, `docs/ward-kdl/ward-kdl.docker.guardfile.md`.
- Podman - not a ward provider. `CONTRIBUTING.md`.

## Agent harnesses

- Claude - shipped. `docs/agent-claude.md`, `docs/agent-drivers.md`.
- Codex - shipped. `docs/agent-codex.md`, `docs/agent-drivers.md`.
- Goose - shipped. `docs/agent-goose.md`, `docs/agent-drivers.md`.
- Opencode - shipped. `docs/agent-opencode.md`, `docs/agent-drivers.md`.
- Aider - shipped as a ward-kdl launcher, not a first-class container harness. `docs/ward-kdl/ward-kdl.aider.guardfile.md`, `docs/ward-kdl-surface.md`.
- Ollama - partial. It is the backend provider for goose/opencode, not a coding harness. `docs/agent-local-model.md`, `docs/agent-local-harnesses.md`.

## Guarded ops providers authored through ward-kdl

- Forgejo - shipped specverb. `docs/ops-forgejo-in-ward.md`, `docs/ward-kdl-surface.md`.
- Tailscale - shipped specverb. `docs/ward-kdl/ward-kdl.tailscale.guardfile.md`, `docs/ward-kdl-surface.md`.
- Trello - shipped specverb. `docs/ward-kdl/ward-kdl.trello.guardfile.md`, `docs/ward-kdl-surface.md`.
- GlitchTip - shipped specverb. `docs/ward-kdl/ward-kdl.glitchtip.guardfile.md`, `docs/ward-kdl-surface.md`.
- SigNoz - shipped specverb. `docs/ward-kdl/ward-kdl.signoz.guardfile.md`, `docs/ward-kdl-surface.md`.
- AWS - shipped execverb. `docs/ward-kdl/ward-kdl.aws.guardfile.md`, `docs/ward-kdl-surface.md`.
- kubectl - shipped execverb. `docs/ward-kdl/ward-kdl.kubectl.guardfile.md`, `docs/ward-kdl-surface.md`.
- Docker - shipped execverb. `docs/ward-kdl/ward-kdl.docker.guardfile.md`, `docs/ward-kdl-surface.md`.
- agents - shipped execverb. `docs/ward-kdl-surface.md`, `docs/ward-kdl-in-ward.md`.
- pkg - shipped specverb. `docs/ward-kdl/ward-kdl.{skillsmp,glama}.guardfile.md`, `docs/ward-kdl-surface.md`.

## Config and auth sources

- `WARD_CONFIG_REF` - shipped. `docs/config-source.md`, `docs/config-ref-resolver.md`.
- `~/.ward/fleet.local.kdl` - shipped operator-local overlay. `docs/fleet-local.md`.
- `WARD_GITHUB_TOKEN_SOURCE` - shipped selector for GitHub token provisioning. `docs/github-token.md`, `docs/agent-github.md`.
- `env` (`WARD_*`, `GH_TOKEN`, `GITHUB_TOKEN`, `SHORTCUT_API_TOKEN`) - shipped operator-local input, not a ward provider. `docs/agent-credentials.md`, `docs/github-token.md`, `docs/shortcut-tracker.md`.
- `SSM` - shipped backing store for Forgejo token, GitHub App key, GlitchTip base URL, SigNoz base URL, Trello creds, and Ollama host. `docs/forgejo-token-audit.md`, `docs/github-token.md`, `docs/ward-kdl/ward-kdl.{glitchtip,signoz,trello,ollama}.guardfile.md`.
- GitHub token sources - shipped. `env`, `gh`, and `app` are the supported sources. `docs/github-token.md`.

## Adding your stack

- GitHub + Issues - reuse the GitHub forge adapter, keep the current runtime, and only split the tracker seam if needed.
- GitLab + Issues - add a forge adapter for GitLab, then keep or split the tracker seam to match the issue API.
- GitHub + Trello - keep the GitHub forge adapter and add a Trello tracker adapter.
- GitLab + Shortcut - add both the GitLab forge adapter and the Shortcut tracker adapter.
- Any stack + podman - keep the higher-level launch flow and replace the container runtime seam.

## On embedding

ward embeds its own launch assets with `go:embed` in `cmd/ward/container.go` and the other `cmd/ward/*assets` bundles. It does not vendor docker, git, or agent CLIs.

## See also

- [CONTRIBUTING.md](../CONTRIBUTING.md)
- [agentsapi.md](agentsapi.md)
- [agent-github.md](agent-github.md)
- [container.md](container.md)
- [container-image.md](container-image.md)
