---
doc_goal: Give a reader the canonical, code-generated list of Ward's fixed agent workflows with each command's purpose and invocation modes, without presenting roles as security principals.
---
# ward agent: the role roster

<!-- Generated from the code roster by `ward agent roster --markdown` (ward#348); do not edit by hand. Regenerate with `make agent-roster`. -->

A flat list of every fixed `ward agent` workflow registered by the binary. A role
word selects workflow mechanics only. It never grants broker operations, credentials,
mounts, network reach, models, identity, or merge authority. Run `ward agent roster`
(`warded roster`) for this list live at the terminal, and the
per-role docs each entry links to carry the prose detail. See
[agent.md](agent.md) for the umbrella and the `warded` public face.

- [`warded engineer`](agent-engineer.md) - Implements a ticket end to end. Modes: A ref carries that issue detached. Freeform text files an issue first, then carries it.
- [`warded director`](agent-director.md) - Opens the attached read-only director surface. Modes: Reads one live queue snapshot, then opens the attached supervision surface. Harness-native goals own repetition and dispatch judgment.
- [`warded qa`](agent-qa.md) - Inspects a candidate and posts a structured verdict comment. Modes: A ref inspects the issue, branch, pull request, and checks, then posts a structured QA verdict.

## See also

- [agent.md](agent.md) - the `ward agent` umbrella and the `warded` public face.
- [agent-subcommands.md](agent-subcommands.md) - the roles compared, the pre-flight, the reaper backstop.
