---
doc_goal: Give a reader the canonical, code-generated list of every ward agent startup role with its tagline and invocation modes, so they can pick engineer, director, advisor, or qa and know it can never drift from the binary.
---
# ward agent: the role roster

<!-- Generated from the code roster by `ward agent roster --markdown` (ward#348); do not edit by hand. Regenerate with `make agent-roster`. -->

A flat list of every `ward agent` startup role - the roster `agentCommand()` registers in
code, rendered by the binary describing itself so the page can never drift. Each role is one
entry: what the specialist does and how you invoke it (a ref acts on an issue, freeform text
files or answers it). Run `ward agent roster` (`warded roster`) for this list live at the
terminal, and the per-role docs each entry links to carry the prose detail. See
[agent.md](agent.md) for the umbrella and the `warded` public face.

- [`warded engineer`](agent-engineer.md) - Implements a ticket end to end. Modes: A ref carries that issue detached, fire-and-forget. Freeform text files an issue first, then carries it. Detached-only - interactive work funnels to the director.
- [`warded director`](agent-director.md) - Autonomously drives a repo's headless lane to drain. Modes: Attached LLM-in-the-loop heartbeat over a repo's backlog (`--repo` scope). Surfaces a read-only scope + dispatch session on drain, no ref.
- [`warded advisor`](agent-advisor.md) - Answers without writing code. Modes: A ref researches the issue and posts the answer as a comment. Freeform text answers inline.
- [`warded qa`](agent-qa.md) - Inspects a candidate and posts a structured verdict comment. Modes: A ref inspects the issue, branch, pull request, and checks, then posts a structured QA verdict comment. Freeform mode is not exposed.

## See also

- [agent.md](agent.md) - the `ward agent` umbrella and the `warded` public face.
- [agent-subcommands.md](agent-subcommands.md) - the roles compared, the pre-flight, the reaper backstop.
