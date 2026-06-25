# ward container

The **container subsystem** spins up an **ephemeral, least-access dev container
per run** to carry a single feature start to merge - implement, commit, merge to
`main`, resolve conflicts, push - then throw the container away. It wraps the
aos-published dev-base image (epic
[ward#98](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/98)).

The user-facing entrypoint is **[`ward agent`](agent.md)**, not a `ward
container` verb: ward#263 retired the hand-run `up`/`exec`/`down`/`ls` leaves,
leaving `ward container` plumbing-only and hidden from `ward --help` (only the
entrypoint-internal `reap`/`bootstrap` remain; manual debug uses raw `docker`).

## The model

Three departures from a transparent, shared, bind-mounted container:
- **One container per run, many at once** - each launch is uniquely named
  (`ward-<repo>-<rand>`); concurrent runs are the default.
- **Fresh clone inside, never on the host** - cached through a shared
  `ward-gitcache` bare mirror, so the host's repo tree stays untouched.
- **Least access** - the only default host bind is the **cwd** (read-only) plus
  ward's entrypoint/doctrine; `~/.aws` is opt-in via `--aws`, and `--host-net`
  opts into the host's tailnet route ([agent-host-net.md](agent-host-net.md)).

## Usage

Launch through [`ward agent`](agent.md):

```bash
ward agent engineer coilyco-gaming/eco-app#123             # carry an issue end to end (detached)
ward agent engineer coilyco-gaming/eco-app#123 --watch     # attached, fire-and-forget off
ward agent engineer coilyco-gaming/eco-app#123 --driver codex --print   # show the docker cmd only
```

`ward agent engineer --help` lists the launch flags the carry brings from this
subsystem (`--driver`, `--aws`, `--watch`, `--print`, `--no-pull`,
`--ward-source`, ...; see [docs/agent-flags.md](agent-flags.md)). The run
attaches by default; the pseudo-TTY (`-t`) is **auto-detected**, on only when
stdin and stdout are both terminals. `--detach`/`headless` background it (`-d`).

## Modes: progressively-less-context ladder

`ward agent`'s `--driver` picks the agent harness *and* how much operating
context is composed, mirroring agent-compose's slices:

- `claude` (default, level 2) - doctrine + the mounted host context (cwd's
  `CLAUDE.md`/`AGENTS.md`).
- `goose` (level 2) - same context as claude, mirrored to `.goosehints`.
- `codex` (level 1) - doctrine + only the cwd's `AGENTS.md`.
- `qwen` (level 0) - doctrine only.

The repo's in-tree `AGENTS.md` loads on top; the level exports as
`WARD_CONTEXT_LEVEL`. codex/goose drop to a shell (not yet imaged); qwen
self-installs opencode (ward#187, [agent.md](agent.md)).

## Inside the container

The entrypoint is embedded in the ward binary and bind-mounted into the
unmodified dev-base image. It configures forgejo git auth, installs ward,
clones the target into `/workspace/<repo>`, installs pre-commit hooks
([container-precommit.md](container-precommit.md)), composes context +
permissions, launches the agent, then reaps. The push token
(`/forgejo/api-token`) resolves **on the host**, via a private 0600
`--env-file`, never in argv or audit.

## Feature-lifetime autonomy + the reaper backstop

The container's top-level doctrine
([AGENTS.container.md](../cmd/ward/containerassets/AGENTS.container.md)) composes
at the top of the agent's context and **overrides** the host harness's hold-backs
(commit/push only when asked, stop for conflicts), so it finishes the whole
feature autonomously, with the container's isolation as the wall (force-push,
other repos, data loss stay out of reach). It is its own
**permission manager** (`bypassPermissions`;
[docs/container-permissions.md](container-permissions.md)), and on every exit the
reaper lands clean work on `main` or salvages it ([reap](container-reap.md)).

## See also

[container-substrate](container-substrate.md) - `/substrate` repos. [FEATURES](FEATURES.md) - inventory. [agent](agent.md) - launch surface. agentic-os `docs/dev-base-image.md`.
