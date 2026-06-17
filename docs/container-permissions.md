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

## The minimal deny wall

`bypassPermissions` still honors `permissions.deny` (and Claude Code's built-in
`rm -rf /` circuit breaker), so the policy keeps a small wall against
agent-error damage to the target repo's `main`:

```json
"deny": [
  "Bash(git push --force:*)",
  "Bash(git push -f:*)",
  "Bash(git reset --hard:*)",
  "Bash(git clean -fd:*)"
]
```

These guard force-push, history rewrites, and hard resets - the
[AGENTS.container.md](../cmd/ward/containerassets/AGENTS.container.md) wall, made
enforceable. Everything inside the normal feature loop is allowed.

## Not in the repo

The deny wall lives **only** in the container's user-level settings, never written
into any target repo. Repos carry no lockdown of their own for the container to
inherit; the container composes its policy fresh each run.

## See also

[docs/container.md](container.md) - the container subsystem.
[docs/container-reap.md](container-reap.md) - the teardown reaper.
