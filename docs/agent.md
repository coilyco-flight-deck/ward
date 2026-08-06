---
doc_goal: Explain the complete public `ward agent` surface and route each durable contract from one entry point.
---
# `ward agent`

`ward agent` launches governed agent work. `warded` is a symlink that rewrites
to the same command.

An agent plan combines four independent selections:

* A role describes the workflow behavior: engineer, director, or QA.
* A harness describes the agent CLI and its host credential or endpoint.
* A workflow describes acceptable landing evidence.
* A context bundle may add read-only instructions and tools without authority.

Roles and context never grant credentials, mounts, network, broker operations,
or merge permission. Ward fixes those surfaces in typed launch and broker code.

## Common commands

```bash
warded engineer owner/repo#123 --print
warded engineer owner/repo#123
warded director --repo owner/repo
warded qa owner/repo#123
ward agent cluster start --harness codex
ward agent run --cluster codex-ab45 --harness codex --role critic \
  --context-bundle /path/to/bundle "Review the proposal."
```

A bare issue ref selects engineer. Freeform engineer text files an issue before
launch. Engineer and QA are detached. Director is an attached read-only
surface. `--print` renders the resolved plan and launches nothing.

## Contracts

* [agent-roster.md](agent-roster.md) and [agent-flags.md](agent-flags.md) - generated command reference.
* [agent-roles.md](agent-roles.md) - role behavior and sealed CI boundary.
* [agent-harnesses.md](agent-harnesses.md) - adapters, auth, model, and endpoint inputs.
* [agent-lifecycle.md](agent-lifecycle.md) - resolution, checks, launch, and outcome.
* [agent-workflow.md](agent-workflow.md) - landing evidence and review.
* [agent-director.md](agent-director.md) - attached supervision.
* [agent-ops.md](agent-ops.md) - read, stop, reap, and retained dispatch operations.
* [agent-dispatch-broker.md](agent-dispatch-broker.md) - durable broker and authority.
* [container-contract.md](container-contract.md) - host/container boundary.
