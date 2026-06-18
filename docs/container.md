# ward container

`ward container` spins up an **ephemeral, least-access dev container per run** to
carry a single feature from start to merge - implement, commit, merge to `main`,
resolve conflicts, push - then throw the container away. It wraps the
aos-published dev-base image. Epic
[agentic-os#220](https://forgejo.coilysiren.me/coilyco-flight-deck/agentic-os/issues/220),
ward [#98](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/98).

## The model

Three departures from a transparent, shared, bind-mounted container:

- **One container per run, many at once.** Every `up` makes a new uniquely-named
  container (`ward-<repo>-<rand>`); many at once is the default.
- **Fresh clone inside, never on the host.** The target is cloned *inside* the
  container, cached through a shared `ward-gitcache` bare mirror, never on the host.
- **Least access.** The only default host bind is the **cwd** (read-only) plus
  ward's embedded entrypoint/doctrine. `~/.aws` is opt-in via `--aws`.

## Usage

```bash
ward container up                         # infer target from cwd's git origin
ward container up coilyco-gaming/eco-app --branch feat/x
ward container up coilyco-gaming/eco-app --print   # show docker cmd, run nothing
ward container ls
ward container exec ward-eco-app-a4154198 -- ward exec test
ward container reap                                # teardown reaper by hand (normally automatic)
ward container down ward-eco-app-a4154198          # keeps the gitcache volume
```

`ward container up --help` lists the full flag set (`--mode`, `--branch`,
`--image`/`--tag`, `--ward-source`, `--aws`, `--detach`, `--print`, `--no-pull`).
The run attaches by default; the pseudo-TTY (`-t`) is **auto-detected**, added
only when stdin and stdout are both terminals (so agent/CI/piped runs drop to
`-i`). `--detach` runs fully backgrounded (`-d`).

## Modes: progressively-less-context ladder

`--mode` picks the agent harness *and* how much operating context is composed,
mirroring agent-compose's slices:

- `claude` (default, level 2) - doctrine + the mounted host context (cwd's
  `CLAUDE.md`/`AGENTS.md`).
- `goose` (level 2) - same full context as claude; the entrypoint mirrors the
  composed doctrine into goose's `~/.config/goose/.goosehints` (goose does not
  read `~/.claude/CLAUDE.md`).
- `codex` (level 1) - doctrine + only the cwd's `AGENTS.md`.
- `qwen` (level 0) - doctrine only.

The repo's in-tree `AGENTS.md` loads natively on top; the level exports as
`WARD_CONTEXT_LEVEL`. codex/qwen/goose binaries aren't in the image yet (claude
is), so the entrypoint drops to a shell for those modes until they land.

## Inside the container

The entrypoint is embedded in the ward binary and bind-mounted into the
unmodified dev-base image. It configures forgejo git auth, installs ward,
cached-fresh-clones the target into `/workspace/<repo>`, installs pre-commit
hooks ([container-precommit.md](container-precommit.md)), composes the mode's
context + permission policy, launches the agent, then reaps on exit. The push
token (`/forgejo/api-token`) resolves **on the host**, injected via a private
0600 `--env-file` removed once docker reads it - never in argv or audit.

## Feature-lifetime autonomy + the reaper backstop

The container's top-level doctrine
([AGENTS.container.md](../cmd/ward/containerassets/AGENTS.container.md)) composes
at the top of the agent's context and **overrides** the host harness's hold-backs
(commit/push only when asked, stop for conflicts), so it finishes the whole
feature autonomously, with the container's isolation as the wall (force-push,
history rewrites, other repos, data loss stay out of reach). It is its own
**permission manager** (`bypassPermissions`, so a headless agent never
auto-denies; [docs/container-permissions.md](container-permissions.md)), and on
every exit `ward container reap` lands clean work on `main` or salvages it
([docs/container-reap.md](container-reap.md)).

## See also

[docs/container-substrate.md](container-substrate.md) - repos warmed into `/substrate`.
[docs/FEATURES.md](FEATURES.md) - inventory. [docs/dispatch.md](dispatch.md) - launch surface.
agentic-os `docs/dev-base-image.md` - the image.
