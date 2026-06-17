# ward container

`ward container` spins up an **ephemeral, least-access dev container per run**
to carry a single feature from start to merge - implement, commit, merge to
`main`, resolve conflicts, push - then throw the container away. It wraps the
aos-published dev-base image. Epic
[agentic-os#220](https://forgejo.coilysiren.me/coilyco-flight-deck/agentic-os/issues/220),
ward [#98](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/98).

## The model

Three deliberate departures from a transparent, shared, bind-mounted container:

- **One container per run, many at once.** Every `up` makes a new uniquely-named
  container (`ward-<repo>-<rand>`); running several at once is the default.
- **Fresh clone inside, never on the host.** The target is cloned *inside* the
  container from forgejo, cached through a shared `ward-gitcache` volume (bare
  mirror). Never bind-mounted, never in the host's tree - a disposable worktree.
- **Least access.** The only default host bind is the **cwd** (read-only) plus
  ward's embedded entrypoint/doctrine. `~/.aws` (broad SSM read) is opt-in via
  `--aws`.

## Usage

```bash
ward container up                         # infer target from cwd's git origin
ward container up coilyco-gaming/eco-app --branch feat/x
ward container up coilyco-gaming/eco-app --print   # show docker cmd, run nothing
ward container ls
ward container exec ward-eco-app-a4154198 -- ward exec test
ward container down ward-eco-app-a4154198          # keeps the gitcache volume
```

`ward container up --help` lists the full flag set (`--mode`, `--branch`,
`--image`/`--tag`, `--ward-source`, `--aws`, `--detach`, `--print`, `--no-pull`).

The run attaches by default; the pseudo-TTY (`-t`) is **auto-detected** and added
only when stdin and stdout are both terminals, so agent/CI/piped runs (and `exec`)
drop to `-i` instead of aborting on "cannot attach stdin to a TTY-enabled
container". `--detach` runs fully backgrounded (`-d`).

## Modes: progressively-less-context ladder

`--mode` picks the agent harness *and* how much operating context is composed,
mirroring agent-compose's per-harness slices:

- `claude` (default, level 2) - doctrine + the mounted host context (cwd's
  `CLAUDE.md`/`AGENTS.md`).
- `codex` (level 1) - doctrine + only the cwd's `AGENTS.md`.
- `qwen` (level 0) - doctrine only.

The repo's in-tree `AGENTS.md` loads natively on top; the level exports as
`WARD_CONTEXT_LEVEL`. codex/qwen binaries aren't in the image yet (claude is), so
the entrypoint drops to a shell for those modes until they land.

## Inside the container

The entrypoint is embedded in the ward binary and bind-mounted into the
unmodified dev-base image (no second image is published). It configures forgejo
git auth, installs ward (the `ward-linux-<arch>` release matching the host's
version, or a `--ward-source` build), cached-fresh-clones the target into
`/workspace/<repo>`, composes the mode's context, and launches the agent. The
push token (`/forgejo/api-token`, user `coilysiren`) is resolved **on the host**
at `up` time and injected via a private 0600 `--env-file` removed once docker
reads it - never in argv, an audit row, or `docker inspect`. `--aws` is only for
in-container `ssm`; the image pull uses `/forgejo/registry-read-token`.

## Feature-lifetime autonomy

The container's top-level doctrine
([AGENTS.container.md](../cmd/ward/containerassets/AGENTS.container.md)) composes
at the top of the agent's context and **overrides** the host harness's default
hold-backs (commit/push only when asked, stop for conflicts, confirm
outward-facing actions), so it finishes the whole feature autonomously. The wall
still stands at force-push, history rewrites, other repos, and data loss.

## See also

[docs/FEATURES.md](FEATURES.md) - inventory. [docs/dispatch.md](dispatch.md) -
sibling agent-launch surface. agentic-os `docs/dev-base-image.md` - the image.
