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

When `WARD_CONFIG_REF` points at a bundle that defines launch defaults, those
defaults own the harness pick before the baked fallback does.

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
warded director --repo owner/name # open the read-only surface
warded director --burndown --repo owner/name # autonomously drain headless work
warded director owner/name#98
```

The bare ref form defaults to `engineer`. The ref can be a bare `#N`, a full
`owner/repo#N`, a full Forgejo issue URL, or, for `director`, an issue-scoped
positional ref.

The director has no separate interactive subcommand. Start `warded director --repo owner/name` from a terminal to open the read-only session from the stored ledger. Add `--burndown` only when the command should autonomously dispatch queued headless work or enumerate the live backlog. During a full burndown lane, press Enter during the sleep offer to open the same session.

## Why the docs are smaller

The old doc tree had one page per issue slice. The new tree keeps only the
release-era path and collapses the details into a small set of durable guides.

## See also

- [first-run.md](first-run.md) - the first dry run.
- [container.md](container.md) - the run box.
