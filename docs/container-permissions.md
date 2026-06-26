# ward container permissions

Inside an ephemeral [`ward container`](container.md), the **container itself is
the permission manager** - not an in-repo lockdown. The entrypoint writes a
user-level `~/.claude/settings.json` (from the embedded
`containerassets/settings.container.json`) before launching the agent.

## Why bypassPermissions

A headless agent (`claude -p`) has no human to answer a permission prompt, so any
tool call that would normally prompt - file edits, builds, `git commit`/`push` -
**auto-denies**, silently breaking the autonomous feature loop. `defaultMode:
bypassPermissions` removes the prompts so the agent can drive the whole feature.

This is safe here because the **container's isolation is the real boundary**, not
the harness permission system:

- a throwaway clone, torn down after the run;
- the host tree mounted read-only (only the cwd, for context);
- a push token scoped to the **one** target repo;
- no other repos cloned or reachable.

The blast radius is the single target repo's own history, in a disposable box.

## No deny wall

The container writes **no** `permissions.deny` list. There is no
harness-level guard against force-push, history rewrites, or hard resets - the
container's isolation (the four bullets above) is the **sole** boundary. This is
the deliberate, more-aggressive posture: the disposable clone, the read-only
host tree, the repo-scoped push token, and the unreachable other repos already
bound the blast radius to one repo's history in a throwaway box, so a redundant
deny list buys nothing and is dropped. The
[AGENTS.container.md](../cmd/ward/containerassets/AGENTS.container.md) wall
(force-push, other repos, data loss out of bounds) stays a doctrine the agent
follows, not an enforced rule.

## Not in the repo

No deny wall lives anywhere - neither in the container's user-level settings
nor in any target repo. Repos carry no lockdown of their own for the container
to inherit; the container composes its policy fresh each run, and that policy is
now `bypassPermissions` with nothing denied.

## See also

[docs/container.md](container.md) - the container subsystem.
[docs/container-reap.md](container-reap.md) - the teardown reaper.
