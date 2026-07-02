# ward container permissions

Inside an ephemeral [`ward container`](container.md), the **container itself is
the permission manager**, not an in-repo lockdown. The entrypoint writes a
user-level `~/.claude/settings.json` (from `containerassets/settings.container.json`)
before launching the agent.

## Why bypassPermissions

A headless agent (`claude -p`) has no human to answer a permission prompt, so any
tool call that would normally prompt - file edits, builds, `git commit`/`push` -
**auto-denies**, silently breaking the autonomous feature loop. `defaultMode:
bypassPermissions` removes the prompts so the agent can drive the whole feature.

This is safe because the **container's isolation plus doctrine are the real
boundary**, not the harness permission system:

- a throwaway clone, torn down after the run;
- the host tree mounted read-only (only the cwd, for context);
- only the repos this run cloned are present on disk - nothing else is fetched;
- the [container doctrine](../cmd/ward/containerassets/AGENTS.container.md) wall
  keeps the agent operating only on those repos.

## Blast radius is parameterized by launch scope

This is **not** a flat "one repo, always" guarantee. How many repos are writable
is set at launch:

- **Default run** (no `--repo`): exactly one repo is cloned - the target - into
  `/workspace/<target>`. The writable blast radius is that single repo's own
  history, in a disposable box.
- **`--repo` run** ([container-multi-repo.md](container-multi-repo.md)): each
  `--repo owner/name` grant clones an **additional** full working copy, with its
  own forgejo push remote and the run's feature branch, and the pre-flight is
  told those grants are writable. Every grant is writable blast radius on top of
  the target - exactly the named grants, never wider, but no longer one repo.

So this doc and [container-multi-repo.md](container-multi-repo.md) describe one
boundary at two scope settings - the single-repo case proves only that a default
run is one repo, not that every warded run is.

## The push token is not the boundary

The container carries a single `FORGEJO_TOKEN` - the **coilyco-ops bot's full
credential**, host-wide, not a per-repo key. Its one credential store
([entrypoint.sh](../cmd/ward/containerassets/entrypoint.sh) `setup_git_auth`)
authenticates a push to **any** repo the bot can write - target and grants
alike. So the token is **not** what pins the blast radius:

- what bounds it is which repos are **cloned and reachable** (the scope above)
  plus the doctrine wall telling the agent to touch only target-plus-grants;
- a repo-scoped / dispatch-only token that would make this a **credential**
  boundary is tracked in
  [ward#318](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/318).

On a default run the blast radius is one repo because only one repo is
**present**, not because the token would refuse a push to the rest.

## The reaper lands only the target

Whatever the scope, the teardown reaper ([container-reap.md](container-reap.md))
pushes only the **target** to `main`. It never lands a `--repo` grant - it only
**verifies** each reached `origin/main`, salvaging and reopening the issue on one
that never did (ward#291). Grants are driven to their push by the agent.

## No deny wall

The container writes **no** `permissions.deny` list - no harness guard against
force-push, history rewrites, or hard resets. The boundary above already bounds
the blast radius, so a deny list buys nothing. The
[AGENTS.container.md](../cmd/ward/containerassets/AGENTS.container.md) wall
(force-push, repos beyond target-plus-grants, data loss) stays doctrine, not
enforced - and with a host-wide token, doctrine is what keeps a run in scope.

## See also

[docs/container.md](container.md) - the container subsystem.
[docs/container-multi-repo.md](container-multi-repo.md) - `--repo` grants that widen the writable blast radius.
[docs/container-reap.md](container-reap.md) - the teardown reaper.
