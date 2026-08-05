---
doc_goal: Record Ward terms that are overloaded, collision-prone, deprecated, or easy to infer incorrectly.
---
# terminology ambiguities

## Deliberate Overloads

`agent` is established in `ward agent`, harness-language, and ordinary prose.
When precision matters, say `role` for engineer/director/qa and `harness` for
claude/codex/goose/opencode.

`drain` means output preservation in current Ward surfaces. The former director
dispatch alias is removed.

`terminal` can mean a TTY, model completion output, or terminal outcome. Say
`TTY`, `completion output`, or `terminal outcome` when more than one could fit.

`release` can mean branch, workflow, artifact, or install channel. Say
`release branch`, `Forgejo release`, or `install channel` when ambiguity
matters.

`flight` language is allowed as analogy. It is not a command-renaming rule.

## Common Collisions

- `dispatch` vs `launch`: dispatch accepts or forwards; launch starts the
  execution path.
- `reservation` vs `launch intent` vs `running engineer`: the issue hold, the
  prelaunch lease, and the visible container are separate.
- `workflow` vs `run`: workflow is the landing policy; run is one execution
  attempt.
- `landing` vs process exit: landing needs repository or PR proof.
- `submitted` and `merge-ready` vs `done`: PR lanes can finish their worker
  obligation before the issue is fully closed.
- `blocked` vs `failed`: blocked needs outside authority or conditions; failed
  attempted and did not land.
- `stop` vs `reap` vs `cleanup`: targeted halt, policy backstop, and state
  removal are different.
- `salvage` vs `rescue`: salvage preserves residual work on a branch/issue;
  rescue preserves Git-object bundles for recovery.
- `read-only` vs no authority: a read-only director cannot push its clone but
  can still inspect, file, dispatch, and broker allowed operations.

## Deprecated Or Compatibility-Only

- New machine-readable issue comments must use `WARD-WORKFLOW:`.
- `WARDED_WORKFLOW:` and older typed `WARD-*` headers are parser compatibility
  only.
- Do not describe `warded` as "warding off" work; it is the public face of the
  guarded agent path.
- Do not treat Docker labels, dispatch artifacts, or local cache as reservation
  authority. The issue thread is canonical.
- Do not say broker acceptance proves an engineer is running.
- Do not rename stable CLI commands during terminology cleanup. File a separate
  issue for compatibility churn.
