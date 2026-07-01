# multi-repo container runs (`--repo`)

By default a `ward container` run is single-repo: it clones one target into
`/workspace/<target>` and carries one feature there ([container.md](container.md)).
A task sometimes spans repos, though - a contract change in one repo and its
consumer in another. `--repo` grants a run **additional writable repos**,
explicitly, so the agent can clone and operate against more than the target.
Epic [ward#230](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/230).
Shortened from `--with-repo` in [ward#280](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/280); that alias was dropped in ward#362, so the flag is now just `--repo`.

This is deliberately opt-in. The container doctrine
([AGENTS.container.md](../cmd/ward/containerassets/AGENTS.container.md)) walls an
agent off from "repos other than the target"; `--repo` is the only way to
widen that wall, and it widens it to exactly the named set - never further.

## Usage

```bash
# Carry an issue in eco-app, also granting write access to eco-protos:
ward agent engineer coilyco-gaming/eco-app#42 --repo coilyco-gaming/eco-protos

# Repeatable; each grant is a bare owner/name or a forgejo clone URL:
ward agent engineer coilyco-gaming/eco-app#42 \
  --repo coilyco-gaming/eco-protos \
  --repo coilyco-flight-deck/cli-guard

# the freeform engineer carry takes the same grants.
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

The teardown reaper ([container-reap.md](container-reap.md)) **lands** only the
target (`$WARD_REAP_WORK`) - it never pushes a granted repo to `main`, so the
agent must drive each grant to its own clean push **before it exits**. It does
**verify** them, though (ward#291): it reads `WARD_EXTRA_REPOS` and checks each
granted clone's `HEAD` reached the freshly-fetched `origin/main`. A grant that
never landed (primary push fires `closes #N` while a non-fast-forward or dead PAT
rejects the secondary) is preserved on a `ward-salvage/<id>` branch and the issue
**reopened** with a recovery comment - surfaced and preserved, not silently lost.

## Plumbing

`--repo` flows host-side -> container as
the space-separated `owner/name` list `WARD_EXTRA_REPOS` (`upPlan.ExtraRepos`,
validated by `parseExtraRepos`). Both bootstrap paths clone the set after the
target: the bash `clone_extra_repos` and the Go `cloneExtraRepos` (ward#181).

## Pre-flight knows the grant

The pre-flight read ([docs/agent-preflight.md](agent-preflight.md)) is fed the `--repo` list and told the
grants are writable, so a cross-repo migration whose deliverable lands in a granted repo is in scope, not a false `NO-GO` (ward#266).

## See also

[docs/container.md](container.md) - the container model and lifecycle.
[docs/container-substrate.md](container-substrate.md) - read-only `/substrate`.
[docs/container-reap.md](container-reap.md) - the teardown reaper.
