# ward agent: the role roster

<!-- Generated from the code roster by `ward agent roster --markdown` (ward#348); do not edit by hand. Regenerate with `make agent-roster`. -->

A flat list of every `ward agent` startup role - the roster `agentCommand()` registers in
code, rendered by the binary describing itself so the page can never drift. Each role is one
entry: what the specialist does and how you invoke it (a ref acts on an issue, freeform text
files or answers it). Run `ward agent roster` (`warded roster`) for this list live at the
terminal; the per-role docs each row links to carry the prose detail. See
[agent.md](agent.md) for the umbrella and the `warded` public face.

| Role | What this specialist does | Invocation modes |
| --- | --- | --- |
| [`warded engineer`](agent-engineer.md) | Implements a ticket end to end. | A ref carries that issue detached, fire-and-forget; freeform text files an issue first, then carries it. Detached-only - interactive work funnels to the director. |
| [`warded architect`](agent-architect.md) | Reads the clone, scopes and dispatches work - but cannot push. | Seedless read-only interactive session; no ref, no issue. |
| [`warded director`](agent-director.md) | Autonomously drives a repo's headless lane to drain. | Attached LLM-in-the-loop heartbeat over a repo's backlog (`--repo` scope); surfaces an interactive session on drain; no ref. |
| [`warded advisor`](agent-advisor.md) | Answers without writing code. | A ref researches the issue and posts the answer as a comment; freeform text answers inline. |

## See also

- [agent.md](agent.md) - the `ward agent` umbrella and the `warded` public face.
- [agent-subcommands.md](agent-subcommands.md) - the roles compared, the pre-flight, the reaper backstop.
