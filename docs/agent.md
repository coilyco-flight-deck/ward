---
doc_goal: Make ward's agent surface understandable from one entry page after the docs collapse.
---
# ward agent

`ward agent` is the guarded execution layer for coding agents.
It launches an ephemeral container, runs the selected role through the chosen
harness, and lands work through the selected workflow.

## Public face
`warded` is the symlinked entrypoint. It rewrites to `ward agent`.

## What it covers

- role selection.
- harness selection.
- launch-time preflight.
- reservation and audit.
- landing policy.
- optional [provider-neutral context-bundle projection](context-bundle.md).
- the check-placement matrix in [agent-check-placement.md](agent-check-placement.md).

## Mental model
Think of `ward agent` as a small pipeline:

1. resolve the issue or freeform prompt.
2. choose the role.
3. choose the harness.
4. check the launch conditions.
5. start the container.
6. follow the selected workflow.

The role determines what the run is trying to do. The harness determines how
the run is authenticated and what model or CLI it talks to. The workflow
determines where the work is allowed to land.
Ward owns typed harness and launch defaults. Supported user configuration may
override only the documented inputs. Use [terminology.md](terminology.md) when
changing these words.

## What changed here

The old issue-slice pages are gone. The durable follow-on docs are:

- [agent-roles.md](agent-roles.md)
- [agent-flags.md](agent-flags.md)
- [agent-harnesses.md](agent-harnesses.md)
- [agent-lifecycle.md](agent-lifecycle.md)
- [agent-director.md](agent-director.md)
- [agent-ops.md](agent-ops.md)
- [agent-workflow.md](agent-workflow.md)
- [warded-kernel-boundary.md](warded-kernel-boundary.md)

## Quick examples

```bash
warded #98
warded engineer #98
warded engineer freeform smoke test
warded director --repo owner/name # open the read-only surface
warded director queue --repo owner/name --json # stable live queue snapshot
warded director owner/name#98
```

The bare ref form defaults to `engineer`. The ref can be a bare `#N`, a full
`owner/repo#N`, a full Forgejo issue URL, or, for `director`, an issue-scoped
positional ref.

Freeform engineer text files an issue first, then carries that issue through
the detached run.

The director has no separate interactive subcommand. Start `warded director --repo owner/name` from a terminal to read one live snapshot and open the attached read-only session. Ward stores no director queue ledger and runs no autonomous loop. A harness-native goal owns repetition and dispatch judgment.

## See also

- [first-run.md](first-run.md) - the first dry run.
- [container.md](container.md) - the run box.
