---
doc_goal: Carry the behavioral detail behind the four ward agent roles - engineer detached, director attached heartbeat, advisor answer-only, qa verdict-only - map the old retired verbs onto them, and explain the shared pre-flight and reaper backstop so an operator knows what each role does and leaves behind.
---
# ward agent subcommands

The roster is four startup roles plus the operational director surface.

- engineer - detached implementation.
- director - read-only supervision.
- advisor - answer-only triage.
- qa - structured verdicts.

The operational backstop lives in the smaller operator docs.

- [agent-ops.md](agent-ops.md)
- [agent-director.md](agent-director.md)
- [agent-lifecycle.md](agent-lifecycle.md)

## The roles

The canonical flat roster lives in [agent-roster.md](agent-roster.md). It is generated from the ward-owned embedded role defaults plus the effective fleet overlays, so it stays current. This doc and the per-role docs ([agent-engineer.md](agent-engineer.md), [agent-director.md](agent-director.md), [agent-advisor.md](agent-advisor.md), [agent-qa.md](agent-qa.md)) carry the prose detail behind each row. Run `warded roster` for the live list.

- **`engineer`** - detached only. A ref runs the agent in print mode to completion and exits into the reaper. From a terminal it first runs a pre-flight check ([agent-preflight.md](agent-preflight.md)): GO launches, NO-GO comments and launches nothing. Freeform text files an issue first, then carries it.
- **`director`** - an attached heartbeat. It polls `WARD-OUTCOME`, dispatches queued issues under `--max-parallel`, and on drain surfaces a read-only scope plus dispatch session.
- **`advisor`** - ref mode researches one-shot and posts the answer as a comment. Freeform answers inside a fresh container and streams inline.
- **`qa`** - opt-in structured inspection. A ref reads the issue, candidate branch or PR, and checks, then posts a verdict comment.

## Pre-flight parity

The engineer runs the same pre-flight ([docs/agent-preflight.md](agent-preflight.md)) in ref and freeform mode. Freeform files the issue first, then gives the same GO / NO-GO read before detaching. A NO-GO comments on the just-filed issue and launches nothing. It honors the same skips (`--print`, `--skip-preflight` / `--no-preflight`, no terminal), and `--skip-preflight` also skips the reservation re-check, update reminder, and network/image pre-start probes. ROUTE's live survey is its own feasibility gate, so ROUTE skips the pre-flight.

## Reaper backstop

The reaper backstop salvages residual work if the agent crashes. The happy path does not rely on it: the agent commits, merges, and pushes itself per its doctrine, finishing to a clean `main` push.

That reaper handles a run that exits. A still-`Up` but wedged `engineer` is the job of the host-side idle-killer [`ward agent reap`](agent-ops.md). A director that wants to halt a specific mis-scoped engineer on demand uses [`ward agent stop`](agent-ops.md).

## See also

- [agent-roster.md](agent-roster.md) - the generated roster page.
