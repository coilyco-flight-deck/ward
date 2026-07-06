---
doc_goal: Make an operator understand `--repo` as the single explicit way to widen the container doctrine wall to a named set of writable repos - what each grant clones, why the agent and not the reaper must land every grant, and how the reaper verifies but never lands them - so a cross-repo run neither under-reaches nor silently drops a grant; and understand the parallel, automatic read-only `catalog.dependsOn` context set, cloned for every role, excluded from that push-verify.
---
# multi-repo container runs (`--repo`)

By default a `ward container` run is single-repo: it clones one target into
`/workspace/<target>` and carries one feature there ([container.md](container.md)).
A task sometimes spans repos, though - a contract change in one repo and its
consumer in another. `--repo` grants a run **additional writable repos**,
explicitly, so the agent can clone and operate against more than the target.
Epic [ward#230](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/230).
Shortened from `--with-repo` in [ward#280](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/280); that alias was dropped, so the flag is now just `--repo`.

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

# the freeform engineer takes the same grants.
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

The shared `ward-gitcache` bare mirror is reused and refreshed under an `flock`
on every run (granted repos move with the feature), the same locking substrate
warming uses so concurrent containers don't race a mirror.

## Read-only context repos (`catalog.dependsOn`, [ward#573](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/573))

Separate from the writable `--repo` grants, **every** warded role (engineer,
director, advisor) auto-mounts the target repo's own declared dependencies as
**read-only reference clones** - the "auto-mount the target's declared deps as
`/substrate`-style reference" the substrate doctrine already blesses. The
container reads the target's `catalog.dependsOn` (from the repo-local
`.ward/ward.yaml`, [docs/ward-yaml.md](ward-yaml.md)) **from the fresh clone it
just made**, not the host cwd, and clones each declared dependency under
`/workspace/<name>` with **no push remote**, **no feature branch**, and a forced
read-only push guard - reference to read while working, never a work surface.
This was advisor-only ([ward#566](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/566)); [ward#573](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/573) widened it to all roles;
[ward#580](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/580) moved the resolution off the host cwd into the container.

The context set is deduped against the target, the writable `--repo` grants (a
repo granted both ways is cloned once, **writable** - the writable grant wins),
and the `/substrate` reference set (a dep already warmed there is not re-cloned).
It uses the longer read-only mirror refresh window so stable upstreams do not
churn the cache on every dispatch.

### External (non-Forgejo) dependencies ([ward#612](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/612))

A `catalog.dependsOn` entry may be a **full git clone URL**, not only a bare
`owner/name`. A bare `owner/name` (or a `forgejo.coilysiren.me/...` URL) stays on
the existing Forgejo-HTTPS-token gitcache path. Any **other** host -
`ssh://git@github.com/StrangeLoopGames/Eco.git`, `git@github.com:owner/name.git`,
or a bare `github.com/owner/name` (synthesized to the sanctioned ssh form) - is
**external**: its host and transport are honored verbatim instead of being thrown
away and force-fed the Forgejo pipeline (the old silent 0-byte-lock failure).

The sealed container has **no egress or ssh key** for an external host, and
mirroring a third party's source onto Forgejo is a **rejected**
redistribution risk, so an external dep is **never** cloned in-container. Its bare
mirror is **seeded host-side over ssh**, into the shared `ward-gitcache` volume,
and the container then does a purely local working clone off that mirror like every
other one. The key stays on the host - the container never talks to the external
forge.

**ward performs that host-side seed itself at launch**, so an external dep needs no
manual warm-up step. Just before the sealed container is created, `ward agent`
resolves the target's external `catalog.dependsOn` entries and, for each mirror not
already in the volume, clones it on the host with a plain `git clone --mirror` over
the **host user's default ssh keychain** - the ambient ssh-agent, then the default
`~/.ssh` identities / `~/.ssh/config`, exactly what the user's own `git clone`
resolves. No ward-specific key is injected or required; whatever ssh access the host
user has is the access ward inherits (Kai's default identity has `StrangeLoopGames/Eco`,
so it just works; any other user gets their own). The clone lands in a host temp dir,
and only the finished bare mirror is copied into the volume by a throwaway `cp`-only
helper - so neither the ssh key nor any egress ever enters a container. The seed is
gated to the real host: an in-container dispatch (which has no key) skips it and
leans on the sealed child's fail-loud.

The seed reads the target's external deps from the host config discovered at the
dispatch cwd. That is a warm-cache hint, **not** the authoritative resolution (the
container still resolves the mounted set from its fresh clone, [ward#580](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/580)):
when the dispatch cwd is the target repo (the common case) its external deps get
pre-seeded, and when it is not, nothing seeds and the container fails loud rather
than mounting empty.

If the host-side seed did not land (no ssh access, no key, or the dispatch cwd was
not the target - so the mirror is absent when the container looks), the dep **fails
loud**: a `MISSING DEPENDENCY:` line naming the dep and why it did not arrive, and
the stale lock is cleared so the gap never reads as "source available". This
replaces the [ward#611](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/611) silent failure, where a declared
`github.com/StrangeLoopGames/Eco` left only an empty `.StrangeLoopGames__Eco.lock`
and the promised sibling `../Eco/` clone never appeared.

Crucially, read-only context repos ride their **own** `WARD_CONTEXT_REPOS` env
key - never `WARD_EXTRA_REPOS`. So the reaper's push-verify (below) never sees
them: a writable engineer run is **not** false-failed for a dependency it only
read, and never gets a push remote it should not have. `WARD_CONTEXT_REPOS` is
**not** passed down from the host, though: the container computes it itself from
the fresh clone ([ward#580](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/580)), so a `warded engineer owner/repo#N` dispatch from an
unrelated host cwd resolves the *target's* deps, never the cwd's or none.

## The reaper boundary

The teardown reaper ([container-reap.md](container-reap.md)) **lands** only the
target (`$WARD_REAP_WORK`) - it never pushes a granted repo to `main`, so the
agent must drive each grant to its own clean push **before it exits**. It does
**verify** them, though: it reads `WARD_EXTRA_REPOS` and checks each
granted clone's `HEAD` reached the freshly-fetched `origin/main`. A grant that
never landed (primary push fires `closes #N` while a non-fast-forward or dead PAT
rejects the secondary) is preserved on a `ward-salvage/<id>` branch and the issue
**reopened** with a recovery comment - surfaced and preserved, not silently lost.

## Plumbing

`--repo` flows host-side -> container as
the space-separated `owner/name` list `WARD_EXTRA_REPOS` (`upPlan.ExtraRepos`,
validated by `parseExtraRepos`). Both bootstrap paths clone the set after the
target: the bash `clone_extra_repos` and the Go `cloneExtraRepos`.

Read-only context repos take a **different route** ([ward#580](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/580)): the host resolves
**nothing** and emits no `WARD_CONTEXT_REPOS`. Instead the container resolves the
set **from the fresh clone**, after cloning the target, through the shared
`resolveCatalogContextRepos` (`catalog.dependsOn` -> dedup against the target, the
writable grants, and `/substrate`). The live bash entrypoint calls the hidden
`ward container resolve-context <clone>` to run that same Go resolver and populates
`WARD_CONTEXT_REPOS` in-container from its output; the Go `cloneContextRepos` path
resolves it in-process. Both then clone read-only via the bash `clone_context_repos`
and Go `cloneContextRepos`. Keeping the `WARD_CONTEXT_REPOS` key distinct from
`WARD_EXTRA_REPOS` is what lets the reaper push-verify the writable set without
ever touching the read-only one.

`WARD_CONTEXT_REPOS` encodes an **external** dep ([ward#612](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/612)) as
`owner/name=<cloneURL>` (a bare `owner/name` for a Forgejo dep), so the honored host
and transport survive the round-trip through the env; both the bash and Go clone
paths split the `=<cloneURL>` back off the slug and honor it, and skip the
in-container mirror clone an external dep can never do.

The **host-side external seed** ([ward#612](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/612))
lives in `agent_context_seed.go` and fires from `createAgentContainer`, right before
the `docker run`. `seedExternalContextMirrors` resolves the target's external deps
(`externalContextDeps`, from the dispatch-cwd config) and, per dep under a host flock,
`git clone --mirror`s over the host's default ssh keychain into a temp dir, then copies
the finished bare mirror into the `ward-gitcache` volume via a `cp`-only helper
(`gitcacheMirrorCopyArgv`). A prior run's mirror is reused (`gitcacheMirrorPresent`
probes the volume with a throwaway `test -d`); a clone or copy failure warns loud so
the container's own fail-loud is never a surprise. The seed is a no-op inside a
container (no host key) - it is the one host-side write into the otherwise
container-computed context path.

## Pre-flight knows the grant

The pre-flight read ([docs/agent-preflight.md](agent-preflight.md)) is fed the `--repo` list and told the
grants are writable, so a cross-repo migration whose deliverable lands in a granted repo is in scope, not a false `NO-GO`.

## See also

[docs/container.md](container.md) - the container model and lifecycle.
[docs/container-permissions.md](container-permissions.md) - the permission posture and blast-radius scope.
[docs/container-substrate.md](container-substrate.md) - read-only `/substrate`.
[docs/container-reap.md](container-reap.md) - the teardown reaper.
