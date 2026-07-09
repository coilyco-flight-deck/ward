---
doc_goal: Map ward's compatibility seams to the real ports and reference adapters so a contributor can extend the right boundary and see what ward embeds instead of vendoring.
---
# Compatibility surface

ward stays small by pushing stack-specific behavior behind a few seams.

## Agent

- **What** - harness contract for Claude, Codex, Goose, and Opencode.
- **Port** - `internal/agentsapi`.
- **Reference adapters** - `internal/agents/{claude,codex,goose,opencode}`.
- **Must satisfy** - `Agent` plus optional capability interfaces, feeding `Manifest`, `RunCtx`, and `HostCtx`.
- **Friction** - medium.

## Forge

- **What** - the git host ward clones from, pushes to, and opens PRs against.
- **Port** - `forge` in `cmd/ward/forge.go`.
- **Reference adapters** - `cmd/ward/forgejo_ops.go` and `cmd/ward/github_ops.go`.
- **Must satisfy** - clone base URL, push username, ref parsing, and PR/MR creation.
- **Friction** - medium.

## Tracker

- **What** - the issue-tracker seam, split off from the git host.
- **Port** - `Tracker` in `cmd/ward/forge.go`.
- **Reference adapters** - the Forgejo and GitHub issue-thread clients.
- **Must satisfy** - create, comment, close/reopen, lock/unlock, and read issue threads without dragging in git-host behavior.
- **Friction** - high for Jira, Linear, Trello, or Shortcut.

## Container runtime

- **What** - the runtime that launches warded containers.
- **Port** - `cmd/ward/container.go` and `cmd/ward/container_compute.go`.
- **Reference adapter** - Docker through the literal `docker` calls in `Runner.dockerExec` and `Runner.dockerCapture`.
- **Must satisfy** - pull, run, inspect, exec, cp, network, and label behavior matching the current flow.
- **Friction** - high.

## Local git and container image

- **What** - the local git CLI and the dev-base image every run starts from.
- **Port** - `cmd/ward/git.go`, `cmd/ward/git_*.go`, and `cmd/ward/container_compute.go`.
- **Reference adapter** - native `git` on PATH and the Forgejo registry image named by `containerImageDefault` and `containerImageTagDefault`.
- **Must satisfy** - git stays a normal host CLI, and the base image stays unmodified with ward and the target repo injected at launch.
- **Friction** - medium for git, high for changing the base image contract.

## cli-guard build dep

- **What** - the upstream policy and routing engine ward compiles against.
- **Port** - the `go.mod` requirement.
- **Reference adapter** - `forgejo.coilysiren.me/coilyco-flight-deck/cli-guard`.
- **Must satisfy** - new engine behavior lands upstream first, then ward bumps the pin.
- **Friction** - low for consumption, high for new engine behavior.

## Adding your stack

- **GitHub + Issues** - reuse the GitHub forge adapter, keep the current runtime, and only split the tracker seam if needed.
- **GitLab + Issues** - add a forge adapter for GitLab, then keep or split the tracker seam to match the issue API.
- **GitHub + Trello** - keep the GitHub forge adapter and add a Trello tracker adapter.
- **GitLab + Shortcut** - add both the GitLab forge adapter and the Shortcut tracker adapter.
- **Any stack + podman** - keep the higher-level launch flow and replace the container runtime seam.

## On embedding

ward embeds its own launch assets with `go:embed` in `cmd/ward/container.go` and the other `cmd/ward/*assets` bundles. It does not vendor docker, git, or agent CLIs.

## See also

- [CONTRIBUTING.md](../CONTRIBUTING.md)
- [agentsapi.md](agentsapi.md)
- [agent-github.md](agent-github.md)
- [container.md](container.md)
- [container-image.md](container-image.md)
