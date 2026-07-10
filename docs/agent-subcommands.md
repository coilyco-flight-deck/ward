---
doc_goal: Carry the behavioral detail behind the four ward agent roles - engineer detached, director attached heartbeat, advisor answer-only, qa verdict-only - map the old retired verbs onto them, and explain the shared pre-flight and reaper backstop so an operator knows what each role does and leaves behind.
---
# ward agent subcommands

The roster is easiest to remember as four startup roles plus the operational
director surface.

- engineer - detached implementation.
- director - read-only supervision.
- advisor - answer-only triage.
- qa - structured verdicts.

The operational backstop lives in the smaller operator docs:

- [agent-ops.md](agent-ops.md)
- [agent-director.md](agent-director.md)
- [agent-lifecycle.md](agent-lifecycle.md)

## The roles

The canonical flat enumeration of the roles - one row each, with the tagline and the
ref-vs-freeform invocation modes - lives in **[agent-roster.md](agent-roster.md)**,
generated from the shipped presets plus the effective fleet role overlays by `ward agent roster`
so it can never go stale. That page is the one source of truth for *which* roles exist; this
doc and the per-role docs ([agent-engineer.md](agent-engineer.md),
[agent-director.md](agent-director.md) + its [surface](agent-surface.md),
[agent-advisor.md](agent-advisor.md), [agent-qa.md](agent-qa.md)) carry the prose detail
behind each row. Run `warded roster` for the list live at the terminal.

The notes below are the behavioral detail the flat roster does not capture:

- **`engineer`** (was `headless` + `task`) - **detached only**: a ref
  runs the agent in print mode (`claude -p` etc.) to completion and exits into the reaper;
  for claude it **streams live progress** to the container log. From a terminal it first runs
  a **pre-flight check** ([agent-preflight.md](agent-preflight.md)): a GO launches, a NO-GO
  comments and launches nothing. Its seed **closes with a "how it felt" comment**
  led by a `WARD-OUTCOME` line. Semantic preset: `read + engineering`. No attach surface (`work`/`--watch` retired);
  interactive work funnels to `director`. Freeform text files an issue first, then carries it:
  DIRECT for an explicit `owner/repo`, ROUTE for a freeform task with no repo.
- **`director`** (was `backlog`) - an attached heartbeat: polls `WARD-OUTCOME`, an LLM
  one-shot picks which queued issues to dispatch under `--max-parallel`, and on drain surfaces
  a **read-only scope + dispatch session** (push credential revoked, reaper skips salvage;
  semantic preset: `read + project-management`;
  [agent-surface.md](agent-surface.md)).
- **`advisor`** (was `reply` + `ask`) - the ref mode researches one-shot and posts the
  answer as a comment; freeform answers *inside* a fresh container and streams it inline.
  Semantic preset: `read`.
- **`qa`** - opt-in structured inspection: a ref reads the issue, candidate branch/PR,
  and checks, then posts a verdict comment. No code edits, no default gating.
  Semantic preset: `read`.

## Pre-flight parity

The engineer runs the **same pre-flight** ([docs/agent-preflight.md](agent-preflight.md))
in both ref and freeform mode: freeform files the issue first, then gives the
same GO / NO-GO read before detaching. A NO-GO comments on the just-filed issue and
launches nothing, leaving a real issue a human can pick up or re-dispatch with
`engineer ... --skip-preflight`. It honors the same skips (`--print`,
`--skip-preflight` / `--no-preflight`, no terminal), and `--skip-preflight` also
cuts the launch-adjacent reservation re-check, update reminder, and network/image
pre-start probes before the container starts. ROUTE's live survey is its
feasibility gate, so ROUTE skips the pre-flight.

## Reaper backstop

The reaper backstop salvages residual work if the agent crashes (it needs ward's jail
off in-container - the entrypoint exports `CLIGUARD_NO_SANDBOX=1`). The
happy path doesn't rely on it: the agent commits/merges/pushes itself per its doctrine,
finishing to a clean `main` push.

That reaper handles a run that **exits**; a still-`Up` but wedged `engineer` is the
job of the host-side idle-killer [`ward agent reap`](agent-reap.md), which stops one
gone log-silent past the threshold. A director that wants to halt a **specific**
mis-scoped engineer on demand (not on idle) uses [`ward agent stop`](agent-stop.md),
which forwards a stop through the dispatch broker - stop-only, engineer-only.

## See also

- [agent-roster.md](agent-roster.md) - the generated roster page.
