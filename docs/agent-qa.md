# warded qa

The QA role inspects a candidate and posts a structured verdict comment.

- It reads the issue, branch, PR, and checks.
- It writes a verdict, not code.
- It is opt-in.
- With `--verification-fixture`, Ward admits only a configured and labeled
  fixture issue, checks out its deterministic remote issue branch, and records
  the exact reviewed commit. A missing branch fails closed.

## See also

- [agent-roster.md](agent-roster.md) - the generated roster entry.
- [agent.md](agent.md) - the entrypoint umbrella.
- [verification-fixtures.md](verification-fixtures.md) - bounded live proof.
