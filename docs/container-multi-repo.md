# multi-repo container runs (`--repo`)

By default a `ward container` run is single-repo: it clones one target into
`/workspace/<target>` and carries one feature there ([container.md](container.md)).
A task sometimes spans repos, though - a contract change in one repo and its
consumer in another. `--repo` grants a run **additional writable repos**,
explicitly, so the agent can clone and operate against more than the target.
Epic [ward#230](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/230);
shortened from `--with-repo` in [ward#280](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/280)
(`--with-repo` stays as a back-compat alias).

This is deliberately opt-in. The container doctrine
([AGENTS.container.md](../cmd/ward/containerassets/AGENTS.container.md)) walls an
agent off from "repos other than the target"; `--repo` is the only way to
widen that wall, and it widens it to exactly the named set - never further.

## Usage

```bash
# Carry an issue in eco-app, also granting write access to eco-protos:
ward agent work coilyco-gaming/eco-app#42 --repo coilyco-gaming/eco-protos

# Repeatable; each grant is a bare owner/name or a forgejo clone URL:
ward agent work coilyco-gaming/eco-app#42 \
  --repo coilyco-gaming/eco-protos \
  --repo coilyco-flight-deck/cli-guard

# headless takes the same grants:
ward agent headless coilyco-gaming/eco-app#42 --repo coilyco-gaming/eco-protos
```

A grant that names the target is a harmless no-op (the target is always cloned).
A malformed ref, or two grants whose repo *names* would collide on the same
`/workspace/<name>` directory, is a hard error at launch - caught host-side
before any container spins.

## What a grant gets you

Each granted repo is cloned as a **full feature working copy** under
`/workspace/<name>`, the same shape as the target:

- a real forgejo push remote (`origin`) with `push.default current`,
- the run's feature branch (`--branch`, default `issue-<N>` on agent runs)
  created in each granted clone too,
- the repo's pre-commit hooks installed (and, on headless runs, the agent-only
  commit suite), so commits hit the same gate a human's would,
- the working tree chowned to the non-root agent user, like the target.

The shared bare mirror in the `ward-gitcache` volume is reused and TTL-agnostic
here: it is refreshed under an `flock` on every run (granted repos are expected
to move with the feature), the same locking substrate warming uses so concurrent
containers don't race a mirror.

## The reaper boundary

The teardown reaper ([container-reap.md](container-reap.md)) backstops **only the
target** (`$WARD_REAP_WORK`). It does **not** salvage, integrate, or push the
granted extra repos. So on a multi-repo run the agent must drive each granted
repo all the way to its own clean push **before it exits** - loose work in an
extra repo is lost, not deferred to a human. The doctrine says this to the agent
in as many words.

## Plumbing

`--repo` (the slice flag still registered under the `with-repo` key for the
back-compat alias) flows host-side -> container as the space-separated `owner/name`
list `WARD_EXTRA_REPOS` (`upPlan.ExtraRepos`, validated by `parseExtraRepos`).
Both bootstrap paths read it and clone the set after the target: the bash
entrypoint's `clone_extra_repos`, and the Go `ward container bootstrap`'s
`cloneExtraRepos` (ward#181). The two stay in parity, like the rest of the
entrypoint port.

## Pre-flight knows the grant

The pre-flight read ([docs/agent-preflight.md](agent-preflight.md)) is fed the
`--with-repo` list and told the grants are writable, so a cross-repo migration
whose deliverable lands in a granted repo is in scope, not a false `NO-GO` (ward#266).

## See also

[docs/container.md](container.md) - the container model and lifecycle.
[docs/container-substrate.md](container-substrate.md) - the read-only `/substrate` references.
[docs/container-reap.md](container-reap.md) - the teardown reaper.
