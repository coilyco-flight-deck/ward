# The warded container API

A [`ward agent`](agent.md) run pulls **one** dev-base image
([container-image.md](container-image.md)) and runs it **unmodified**. What turns a
bare image into a warded feature container is handed in at bring-up: ward
bind-mounts its own [`entrypoint.sh`](../cmd/ward/containerassets/entrypoint.sh) and
passes a set of `WARD_*` env vars, and the entrypoint turns those into a
ready-to-work clone. That handoff **is** the container API - the stable contract
between host `ward` (`docker run` composed in
[`container_compute.go`](../cmd/ward/container_compute.go)) and the entrypoint that
reads it. None of it is baked into the image, so an image pin never changes this
contract - only a ward upgrade does.

Three interface surfaces, plus the capability ladder they key off:

- **Bind mounts** and the **produced file layout** - below.
- **[The `WARD_*` env contract](container-env.md)** - the non-secret config the entrypoint reads.
- **[The progressive-capability ladder](container-capability-ladder.md)** - how much context a run gets, by driver.

## Bind mounts

Ward mounts **least-access by default**: the target repo is **never** bind-mounted
(it is fresh-cloned inside), and every mount beyond the core three is an explicit
opt-in (`leastAccessMounts` / `dockerSockMount` in
[`container_compute.go`](../cmd/ward/container_compute.go)).

Always mounted:

- **host cwd -> `/opt/ward-context`** (ro, `WARD_CONTEXT_SRC`) - the operating context;
  the composer reads its `CLAUDE.md` / `AGENTS.md` at level >= 1.
- **`ward-gitcache` volume -> `/gitcache`** (rw, `WARD_GITCACHE`) - the shared bare-mirror
  cache that makes a fresh clone warm.
- **assets dir -> `/opt/ward`** (ro) - ward's embedded container assets: `entrypoint.sh`,
  [`AGENTS.container.md`](../cmd/ward/containerassets/AGENTS.container.md),
  the `bypassPermissions` policy, the substrate manifest.

Opt-in mounts (off unless the flag is set):

- **`~/.aws` -> `/root/.aws`** (ro) - `--aws`, the broad SSM read surface.
- **ward checkout -> `/opt/ward-src`** (ro) - `--ward-source`; build ward from source. Sets `WARD_FROM_SOURCE`.
- **agent-log drain -> `/opt/ward-agent-logs`** (ro) - the director's redacted log read ([agent-surface-log-read.md](agent-surface-log-read.md)).
- **`/var/run/docker.sock`** (rw) - the surface dispatch path ([agent-surface.md](agent-surface.md)).

## What the entrypoint produces

Given the mounts and env, [`entrypoint.sh`](../cmd/ward/containerassets/entrypoint.sh)
runs as root and produces what the agent launches into:

- **`/workspace/<repo>`** - the fresh target clone, warm through the `/gitcache` mirror.
  The authoritative work surface; `origin` is a real push remote.
- **`/workspace/<name>`** - one full clone per `WARD_EXTRA_REPOS` grant ([container-multi-repo.md](container-multi-repo.md)).
- **`/substrate/<name>`** - read-only-by-convention manifest-repo copies ([container-substrate.md](container-substrate.md)).
- **pre-commit hooks installed** in each work clone ([container-precommit.md](container-precommit.md)).
- **`~/AGENTS.md`** - the composed context (doctrine, then host context per the ladder,
  then a read-only overlay on a surface run); each harness's load point links to it.
- **`~/.claude/settings.json`** - the `bypassPermissions` policy ([container-permissions.md](container-permissions.md)).
- **per-mode credentials + config** from a decoded secret ([agent-credentials.md](agent-credentials.md)).
- **the reaper armed** on the `EXIT` trap ([container-reap.md](container-reap.md)).

It then drops to the non-root agent user and launches the mode's argv.

## See also

- [container-env.md](container-env.md) - the full `WARD_*` environment contract.
- [container-capability-ladder.md](container-capability-ladder.md) - the progressive-capability ladder.
- [container.md](container.md) - the container model and lifecycle these surfaces serve.
- [container-image.md](container-image.md) - the one image this contract runs unmodified.
