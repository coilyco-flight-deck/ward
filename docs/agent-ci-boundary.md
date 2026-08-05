---
doc_goal: Define how sealed engineer and QA runs consume live CI evidence and hand live-only failures to operators.
---
# Agent CI boundary

Engineer and QA runs may inspect live CI status and logs as read-only evidence.
They do not debug, rerun, or iterate against the live pipeline.

## Engineer

The engineer may make at most one corrective push, and only when local
repository evidence proves the change. A failing check is not proof by itself.

If a failure exists only in live CI or recurs after that push, the engineer
files a separate `interactive` issue and reports the current workflow blocked.
The handoff records:

* the exact run.
* the first actionable error.
* the local proof state.
* the operator verification request.

## QA

QA may inspect the same status and logs. QA remains read-only, never changes the
candidate, and requires the same operator handoff for a live-only or repeated
failure.

## Authority

Director and Ops retain live remediation authority. The director merge sweep
stays separate from engineer and QA launch authority.

See [agent-workflow.md](agent-workflow.md) and
[agent-roles.md](agent-roles.md).
