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
  container (`ward-<repo>-<rand>`). Running several at once - same or different
  repos - is the default, not an edge case. There is no singleton.
- **Fresh clone inside, never on the host.** The target repo is cloned *inside*
  the container from forgejo, cached through a shared `ward-gitcache` named
  volume (a bare mirror, refreshed each run). It is never bind-mounted and never
  lands in the host's repo tree - like a worktree, but disposable.
- **Least access.** The only default host bind is the **cwd** (read-only, for
  context) plus ward's embedded entrypoint/doctrine. `~/.aws` (broad SSM read)
  is opt-in via `--aws`.

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

## Modes: progressively-less-context ladder

`--mode` picks the agent harness *and* how much operating context is composed,
mirroring agent-compose's per-harness slices:

- `claude` (default, level 2) - doctrine + the mounted host context (cwd's
  `CLAUDE.md`/`AGENTS.md`).
- `codex` (level 1) - doctrine + only the cwd's `AGENTS.md`.
- `qwen` (level 0) - doctrine only.

The repo's own in-tree `AGENTS.md` loads natively on top regardless. The level
is exported as `WARD_CONTEXT_LEVEL`. The codex/qwen agent *binaries* are not yet
in the dev-base image (claude is); their ladder plumbing is wired now and the
entrypoint drops to a shell for those modes until the binaries land.

## Inside the container

The entrypoint is embedded in the ward binary and bind-mounted into the
unmodified dev-base image (no second image is published). It configures forgejo
git auth, installs ward (downloads the `ward-linux-<arch>` release asset
matching the host ward's version, or builds from a `--ward-source` mount),
cached-fresh-clones the target into `/workspace/<repo>`, composes the mode's
context, and launches the agent. The git push token (`/forgejo/api-token`, user
`coilysiren`) is resolved **on the host** at `up` time and injected via a
private 0600 `--env-file` removed once docker reads it - never in argv, an audit
row, or `docker inspect`. `--aws` is only for in-container `ssm`; the image pull
uses `/forgejo/registry-read-token` (host must be `docker login`'d).

## Feature-lifetime autonomy

The container's top-level doctrine
([cmd/ward/containerassets/AGENTS.container.md](../cmd/ward/containerassets/AGENTS.container.md))
is composed at the top of the agent's context and **overrides** the host
harness's default hold-backs ("commit/push only when asked", "stop for
conflicts", "confirm outward-facing actions"), so the container finishes the
whole feature autonomously. The wall still stands at force-push, history
rewrites, other repos, and data loss.

## See also

[docs/FEATURES.md](FEATURES.md) - inventory. [docs/dispatch.md](dispatch.md) -
sibling agent-launch surface. agentic-os `docs/dev-base-image.md` - the image.
