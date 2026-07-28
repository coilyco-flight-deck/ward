---
doc_goal: Keep the container-reap anchor stable after the old page was collapsed.
---
# container reap

This page is the durable anchor for the reaper backstop.

- It describes the exit-time and idle-time salvage path.
- It is the safety net after the run has already left the launch path.
- It is about the teardown action, not the launch decision.

## Manual proof: issue 1605

On 2026-07-28, [issue 1605](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1605)
was manually proven complete before the reaper reopened it:

- the engineer pushed `main` at `626969b2`.
- commit `a7cef736` on `main` carried `closes #1605`.
- the issue thread already had `WARD-WORKFLOW: done ✅`.
- Forgejo promote run `#2507` and release run `#2508` were green.
- the salvage branch had no diff against `main`, so no pull request opened.

That state is a completed run, not residual work. A clean no-diff reap must
re-check `origin/main` for the carried same-repo closing reference before it
posts a blocking salvage outcome or reopens the carried issue.

## See also

- [container-lifecycle.md](container-lifecycle.md) - launch, debug, teardown.
- [agent-ops.md](agent-ops.md) - logs, stop, list, reap.
