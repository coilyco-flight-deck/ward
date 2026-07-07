---
doc_goal: Convey the container subsystem as the wall that makes ward's guarded-execution half real - an ephemeral, least-access, one-per-run box whose isolation (not a permission prompt) is the boundary carrying a feature start-to-merge, with the fresh-clone / least-bind / reaper-backstop model that bounds an autonomous agent's reach.
---
# ward container

The **container subsystem** spins up an **ephemeral, least-access dev container
per run** to carry a single feature start to merge - implement, commit, merge to
`main`, resolve conflicts, push - then throw the container away (epic
[ward#98](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/98)).

The user-facing entrypoint is **[`ward agent`](agent.md)**, not a `ward
container` verb: the hand-run `up`/`exec`/`down`/`ls` leaves were retired,
leaving `ward container` plumbing-only and hidden from `ward --help` (only the
entrypoint-internal `reap`/`bootstrap` remain; debug uses raw `docker`).

## The model

Three departures from a transparent, shared, bind-mounted container:
- **One container per run, many at once** - named for its role
  (`engineer-<driver>-<repo>-<N>`); `ward.*` labels carry identity.
- **Fresh clone inside, never on the host** - cached through a shared
  `ward-gitcache` bare mirror, so the host's repo tree stays untouched.
- **Least access** - the only default host bind is the **cwd** (read-only) plus
  ward's entrypoint/doctrine; `~/.aws` and the tailnet route
  ([agent-host-net.md](agent-host-net.md)) are opt-in **per startup role** via
  the role's guardfile set in [`ward-kdl.fleet.kdl`](../.ward/ward-kdl/ward-kdl.fleet.kdl)
  ([ward#578](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/578)) - the advisor holds them, the engineer does not. See
  [agent-flags.md](agent-flags.md).

## Usage

Launch through [`ward agent`](agent.md):

```bash
ward agent engineer coilyco-gaming/eco-app#123          # carry an issue end to end (detached)
ward agent engineer coilyco-gaming/eco-app#123 --print  # show the docker cmd only (dry run)
```

`ward agent engineer --help` lists the launch flags (`--driver`,
`--print`, `--repo`, ...; see [agent-flags.md](agent-flags.md)). The
**engineer always detaches**: interactive work goes to the
[director](agent-director.md), whose surface owns the attached auto-TTY shape
([agent-surface.md](agent-surface.md)).

## Modes: progressively-less-context ladder

`ward agent`'s `--driver` picks the harness **and** its context level:
`claude`/`goose` at level 2 (doctrine + cwd
`CLAUDE.md`/`AGENTS.md`), `codex` at level 1 (cwd `AGENTS.md` only), `opencode` at
level 0 (doctrine only, self-installing). The level exports as
`WARD_CONTEXT_LEVEL`; the in-tree `AGENTS.md` loads on top ([agent.md](agent.md)).
Full ladder + the container env/mount API: [container-api.md](container-api.md).

## The image

Every run pulls **one** image, run unmodified: the aos-published dev-base
**`forgejo.coilysiren.me/coilyco-flight-deck/agentic-os:latest`**, on the
**Forgejo container registry** at `forgejo.coilysiren.me`. **Anonymous pull
works** - a cold host `docker pull`s it with no login. `:latest` is a **moving
tag** (each aos release also tags `vX.Y.Z`); pin off it with `--image` / `--tag`
or `WARD_AGENT_IMAGE` / `WARD_AGENT_TAG`. Full registry / tag-policy /
provenance detail: [container-image.md](container-image.md).

## Inside the container

The entrypoint is embedded in the ward binary and bind-mounted into the
unmodified image. It configures forgejo git auth, installs ward, clones the
target into `/workspace/<repo>`, installs pre-commit hooks
([container-precommit.md](container-precommit.md)), composes context +
permissions, launches the agent, then reaps. The push token - the `coilyco-ops`
bot's, from SSM `/forgejo/coilyco-ops/api-token`, not a personal PAT ([ward#161](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/161)) -
resolves **on the host**, via a private 0600 `--env-file`, never in argv or
audit, so the container's clones, commits, and pushes attribute to the bot.

## Feature-lifetime autonomy + the reaper backstop

The container's top-level doctrine
([AGENTS.container.md](../cmd/ward/containerassets/AGENTS.container.md)) composes
atop the agent's context and **overrides** the host harness's hold-backs, so it
finishes the whole feature autonomously with the container's isolation as the
wall. It is its own
**permission manager** (`bypassPermissions`;
[container-permissions.md](container-permissions.md)); on exit the reaper lands
clean work on `main` or salvages it ([reap](container-reap.md)).

## See also

[container-image](container-image.md) - the image ward pulls (ref, registry, tags). [container-substrate](container-substrate.md) - `/substrate` repos. [FEATURES](FEATURES.md) - inventory. [agent](agent.md) - launch.
