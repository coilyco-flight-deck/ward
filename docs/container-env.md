---
doc_goal: Give a reader the full non-secret WARD_* env contract the entrypoint reads - grouped by purpose, with the secrets-ride-a-private-env-file boundary made explicit - so the auditable config surface and what deliberately stays off it are both legible.
---
# The warded container `WARD_*` env contract

`wardEnv` in [`container_compute.go`](../cmd/ward/container_compute.go) assembles the
**non-secret** config the [entrypoint](../cmd/ward/containerassets/entrypoint.sh) reads.
It is safe to print and record in the audit row - the token never appears here (secrets
ride a private `--env-file`, below). One surface of the [container API](container-api.md).

## Identity and target (always set)

- `WARD_CONTAINER_NAME` - the friendly `docker --name`, for the in-container status line.
- `WARD_TARGET_REPO` / `WARD_TARGET_OWNER` / `WARD_TARGET_NAME` - the `owner/name` slug and its split halves.
- `WARD_MIRROR_NAME` - the bare-mirror directory name under `/gitcache`.
- `WARD_TARGET_ISSUE` - the issue number, used for the reaper's pre-launch-hold release.
- `WARD_BRANCH` - the feature branch to check out, when the run names one.

## Forge and clone base

- `WARD_FORGEJO_BASE` - the Forgejo base URL. Always set; ward's release source and the **default** clone base.
- `WARD_FORGE` / `WARD_CLONE_BASE` - set **only** on a GitHub run ([agent-github.md](agent-github.md)): `WARD_FORGE=github` clones off `github.com` with the `x-access-token` push user. Forgejo runs emit neither.

## Driver, agent, and context

- `WARD_MODE` - the driver (`claude` / `codex` / `goose` / `opencode`; `qwen` aliases `opencode`).
- `WARD_AGENT` - the in-container binary launched for that mode.
- `WARD_CONTEXT_LEVEL` - the capability-ladder rung (`2` / `1` / `0`; [container-capability-ladder.md](container-capability-ladder.md)).
- `WARD_CONTEXT_SRC` - the host-cwd mount the composer reads host context from.
- `WARD_VERSION` - the ward release tag to install (or `dev` / source build).
- `TERM` / `COLORTERM` - 256-color + truecolor so the agent keeps color.

## Substrate ([container-substrate.md](container-substrate.md))

- `WARD_SUBSTRATE_SEED` / `WARD_SUBSTRATE_DEST` - the baked seed dir, and where working copies land (`/substrate`).
- `WARD_SUBSTRATE_MANIFEST` / `WARD_SUBSTRATE_TTL` - the `owner/name tier` warm-list, and the refresh TTL (seconds).

## Run-shape flags (set only when that shape is active)

- `WARD_HEADLESS=1` - a detached engineer run: one-shot, streamed as `stream-json`.
- `WARD_ASK=1` - an advisor freeform run: a plain one-shot answer.
- `WARD_READONLY=1` - the director's read-only surface: push wiring stripped ([agent-surface.md](agent-surface.md)).
- `WARD_EXTRA_REPOS` - a space-separated `owner/name` grant list ([container-multi-repo.md](container-multi-repo.md)).
- `WARD_DISPATCH_BROKER_ADDR` / `WARD_DISPATCH_BROKER_TOKEN` - the host dispatch broker a surface dials.
- `WARD_TS_SOCKS5` + the `WARD_TOWER_*` set - the `--ts-sidecar` tailnet route ([agent-tailnet-topology.md](agent-tailnet-topology.md)).
- `WARD_FROM_SOURCE` - the `/opt/ward-src` mount; build ward from source not release.
- `WARD_USE_GO_BOOTSTRAP=1` - experimental hand-off to the Go bootstrap.

Entrypoint-tunable fallbacks (set to override): the commit-identity (`WARD_GIT_NAME` /
`WARD_GIT_EMAIL`, [agent-attribution.md](agent-attribution.md)), the agent user
(`WARD_AGENT_UID` / `_GID` / `_HOME`), the local-harness model bindings, and
`WARD_SMOKE_TEST_SKIP` / `WARD_SUBSTRATE_SKIP`.

## Secrets ride the `--env-file`, never argv

The token and credential channel is deliberately **not** in `wardEnv`. It rides a private
`0600` `--env-file` so it never lands in argv or the audit row
([agent-credentials.md](agent-credentials.md)): `FORGEJO_TOKEN` (or `GITHUB_TOKEN`), and
the base64 credential blobs the entrypoint decodes to disk then **scrubs from the env** -
`WARD_CLAUDE_CREDS_B64`, `WARD_CODEX_AUTH_B64`, `WARD_GOOSE_OLLAMA_HOST_B64`.

## See also

- [container-api.md](container-api.md) - the API overview (mounts + file layout).
- [container-capability-ladder.md](container-capability-ladder.md) - what `WARD_CONTEXT_LEVEL` selects.
- [agent-tailnet-topology.md](agent-tailnet-topology.md) - the `WARD_*` network-topology overrides.
