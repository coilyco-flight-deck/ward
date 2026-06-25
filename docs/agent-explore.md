# ward agent explore

`ward agent explore` is the **read-only** sibling of
[`ward agent sandbox`](agent-sandbox.md): the same seedless interactive bring-up -
a fresh ephemeral container with a fresh clone plus the composed operating context,
dropping you into a live agent with **no issue and no seed** - except the run gets
**no push credential**. The agent can read the repo, search it, and run read-only
commands, but it **cannot mutate the canonical remote**: no commit-and-push, no
merge to `main`, no PRs or issues.

Where `sandbox` is a full writable scratch box (it gets the same push token a `work`
carry gets), `explore` is for when you want a hard guarantee that nothing leaves the
box - a safe read of unfamiliar or sensitive code, a review-by-conversation, a spike
you do not want pushed by accident.

## Usage

```bash
warded explore                                     # claude, read-only, fresh clone of the cwd's repo
ward agent explore --repo coilyco-flight-deck/ward
ward agent explore --driver codex                  # pick another harness
ward agent explore --repo coilyco-flight-deck/ward --print   # show the docker plan, run nothing
```

It takes no positional argument. The flags mirror [`sandbox`](agent-sandbox.md):
`--repo`, `--with-repo`, `--image`, `--tag`, `--ward-source`, `--aws`, `--print`,
`--no-pull`, `--driver`.

## How read-only is enforced

Three layers, one soft and two the agent cannot route around:

1. **Composed restriction (soft).** A seedless session has no prompt to carry the
   "do not push" rule, so it rides in the **operating context**: the entrypoint
   appends a read-only block to the composed `CLAUDE.md` that overrides the container
   doctrine's "implement, commit, merge, push" mandate (the static entry context the
   surface needs, ward#293).
2. **Revoked push credential (hard).** The clone still happens with the bot
   credential (private repos clone), but **before the agent drops** the entrypoint
   removes `/etc/ward-git-credentials`, drops the system `credential.helper`, and
   unsets `FORGEJO_TOKEN`. After that a `git push` - or a hand-built authenticated
   URL - has nothing to authenticate with.
3. **Reaper skips salvage.** The reaper, which normally pushes whatever the agent
   left behind, short-circuits when `WARD_READONLY` is set - so the teardown backstop
   cannot undo the guarantee.

The agent can still `git commit` locally (harmless); only the network mutation is
blocked. On exit the throwaway clone is swept by the [reaper](container-reap.md).

### The pre-push message layer (ward#299)

On its own the revoked credential surfaces an **opaque** failure: `git push` dies
with a generic `could not read Username`, and a drifted session burns turns on
it. So a read-only session also lands a per-clone `pre-push` git hook (on the work
clone and each `--repo` extra) that fires **before** git reaches the remote:

```
ward: read-only explore session - push is disabled (ward#293).
Nothing leaves this container. Commit/branch locally all you like.
```

This is purely the **clear-message layer**: the hook is bypassable (`--no-verify`,
or `rm`), and the revoked credential stays the backstop. A git-level hook (not a
`PreToolUse` hook) covers every driver, and per-clone (not `core.hooksPath`)
avoids shadowing the pre-commit install. Tradeoff: an ad-hoc mid-session clone
does not inherit it. Both the entrypoint and Go bootstrap install it.

## `--print`

Resolves the repo and renders the docker plan, then exits without pulling, cloning,
or running anything. The plan prints `access: read-only (push credential revoked
after clone)` and `WARD_READONLY=1`. Safe with no docker daemon.

## See also

- [docs/agent-sandbox.md](agent-sandbox.md) - `sandbox`, the writable sibling.
- [docs/agent.md](agent.md) - the `ward agent` umbrella.
- [docs/agent-ask.md](agent-ask.md) - `ask`, the one-shot read-only question surface.
- [docs/container-reap.md](container-reap.md) - the reaper that sweeps the run on exit.
