---
doc_goal: Describe the native PR-workflow tools (merge, per-PR CI status, Actions run status, rerun) and the embedded role x workflow permission table that gates them.
---
# ward agent pr

`ward agent pr` carries PR-workflow management as **native ward code** on the
compiled Forgejo client, gated by ward's **embedded role permission system**
([ward#1067](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1067)). None of it routes through the runtime KDL specgen surface, so a
stripped or rolled-back `.ward/` guardfile bundle cannot disable it - the
config-inversion that let the 2026-07-10 permissions rollback silently strip a
director's merge and CI-status reach ([infrastructure#538](https://github.com/coilysiren/infrastructure/issues/538)).

## The verbs

- `ward agent pr status <owner/repo#N>` - one PR head's combined CI status
  (`GET /repos/{owner}/{repo}/commits/{ref}/status`), plus the base branch's
  required contexts.
- `ward agent pr merge <owner/repo#N> [--style STYLE]` - merge one PR:
  permission gate, live required-status gate, a merge pinned to the checked
  head commit, smart-defaults style selection when set, repo-default fallback
  when allowed, delete-branch propagation, then the merged-state
  postcondition. Ward requires `merged: true`
  (`GET /repos/{owner}/{repo}/pulls/{index}/merge`).
- `ward agent pr runs [owner/repo] [--limit N]` - Actions runs with per-run
  conclusions (`GET /repos/{owner}/{repo}/actions/runs`).
- `ward agent pr rerun <owner/repo> <run-id>` - rerun one Actions run. The
  pinned Forgejo API has no rerun operation yet ([agentic-os#434](https://github.com/coilysiren/agentic-os/issues/434)), so on today's
  forge this degrades **loudly** with the manual retrigger fallback; the day the
  forge ships the API leaf, the compiled client starts working with no spec
  bump.

## The permission table

Merge authority is product data in the embedded role catalog
(`merge-authority` in the shipped roles KDL), keyed to the workflow-mode model:

- `pull-request` - the **director** may merge (the PR is the merge gate).
- `pull-request-and-merge` - the **engineer** self-merges; the director's
  sweep also lands it.
- `remote-branch-only` / `merge-remote-main` - merge withheld from every role.

Status and runs are read verbs (any catalog role with `read`). Rerun needs an
`engineering` or `project-management` role, so the advisor and QA roles cannot
poke CI. An unknown role is denied everything, fail-closed.

When a PR repair path is failing, ward now prints a concrete bucket first:
`ci-parity-gap`, `main-red`, `merge-queue-churn`, or `pr-regression`. That
bucket is carried into the PR repair seed and the status readout so the next
step names the actual failure mode instead of launching another vague repair
loop.

PR mode: the engineer stamps `ward.workflow:` into a `pull-request-and-merge` PR body. No marker means plain `pull-request`. The comment starts with the PR URL. Review-gated runs carry `director merge authorization: reviewed-and-ready`. PR merges can also take `smart-defaults > pr-merge-style`.

The recovery and execution-placement details live in
[agent-pr-workflow-recovery.md](agent-pr-workflow-recovery.md).

## See also

- [agent-director.md](agent-director.md) - the read-only director lane.
- [agent-dispatch-broker.md](agent-dispatch-broker.md) - the broker channel.
- [agent-workflow.md](agent-workflow.md) - the workflow-mode model.
- [agent-roles.md](agent-roles.md) - role semantics.
