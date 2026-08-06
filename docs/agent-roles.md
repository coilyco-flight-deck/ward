---
doc_goal: Define Ward's fixed roles, execution limits, exact-revision QA contract, and sealed worker CI boundary without implying authority.
---
# Agent roles

Ward exposes three fixed workflow roles. A role changes prompt and execution
behavior only. It cannot change credentials, mounts, network, broker grants,
merge authority, model, identity, or container topology.

## Engineer

* Implements one issue end to end in a detached container.
* Has a 90-minute execution limit.
* Accepts issue refs, pull-request refs, and freeform work that Ward first files
  as an issue.
* Follows the selected landing workflow and leaves durable remote evidence.

## Director

* Opens an attached, read-only supervisory surface with no execution limit.
* Reads live queue and run state and may invoke explicitly typed broker actions.
* Does not poll, prioritize, or autonomously dispatch. A harness-native goal
  owns repeated judgment.

## QA

* Independently inspects one exact candidate revision in a detached container.
* Has a 30-minute execution limit and writes a structured verdict, not code.
* Records the reviewed revision. A landing gate accepts the verdict only while
  it still matches the candidate head.

## Sealed worker CI boundary

Engineer and QA may read live CI status and logs. QA never changes a candidate.
An engineer may make one corrective push only when local repository evidence
proves it. A live-only failure or a recurrence after that push is operator
work, recorded in a separate `interactive` issue with the exact run, first
actionable error, local proof state, and requested verification. Neither role
reruns or probes live CI as a debugging loop.

## See also

* [agent-director.md](agent-director.md) - director surface.
* [agent-workflow.md](agent-workflow.md) - landing modes and review.
* [agent-harnesses.md](agent-harnesses.md) - independent harness axis.
