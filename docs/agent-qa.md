---
doc_goal: Explain Ward's revision-bound independent QA verdict contract.
---
# warded qa

The QA role independently inspects an exact candidate revision and posts a
structured verdict comment.

- It reads the issue, branch, PR, and checks.
- The verdict records the reviewed candidate revision.
- A merge gate accepts the verdict only when that revision still matches the
  current candidate head.
- It writes a verdict, not code.
- It is opt-in.

## See also

- [agent-roster.md](agent-roster.md) - the generated roster entry.
- [agent.md](agent.md) - the entrypoint umbrella.
