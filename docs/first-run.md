---
doc_goal: Give a short, reliable path from zero to a first dry run without the old harness-slice detours.
---
# first run

Start here for a first `warded` run.

## What you need

- Docker running.
- A reachable issue tracker for the target repo.
- The repo's `.ward/ward.yaml` if you are using the dev-verb gate.

If you need a concrete repo target for examples that should actually resolve,
use `coilysiren/example` or its GitHub URL. It is a public placeholder repo,
not a deployment prerequisite.

## The safe first command

```bash
warded engineer #98 --print
```

`--print` shows the launch without starting the container.

## What happens next

- `ward agent` resolves the role and harness.
- The launch path posts the reservation and runs preflight.
- The detached run writes audit and log output.

## A good first pass

1. start with `--print`.
2. confirm the resolved ref.
3. confirm the selected workflow.
4. confirm the chosen harness.
5. drop `--print` only after the command shape looks right.

If you are starting from a fresh host-local setup, run `ward setup` once to
create `~/.ward/config.yaml`, replace the placeholder scope, and restart
`warded` before the next launch.

## What not to expect

- this page is not the full launch reference.
- it is not the container contract.
- it is not the workflow policy.

## See also

- [agent.md](agent.md) - the entrypoint.
- [agent-lifecycle.md](agent-lifecycle.md) - the launch path.
