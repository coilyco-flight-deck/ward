---
doc_goal: Keep the director triage anchor stable after the old page was collapsed.
---
# director startup triage

This page is the durable anchor for the director's startup triage pass.

- It labels open issues before an opt-in `--burndown` run dispatches.
- Plain `warded director` does not enumerate live issues unless `--triage` or
  `--burndown` is set.
- Plain `warded director` does not run startup triage unless `--triage` is set.
- It turns the backlog into a prioritized dispatch surface.
- It is the preflight triage step, not the merge policy itself.

## See also

- [agent-director.md](agent-director.md) - the read-only director lane.
- [agent-workflow.md](agent-workflow.md) - the landing policy.
