# ward agent: the role roster

The `ward agent` roles differ in what they do, how attached they are, and what they
leave behind. See [docs/agent.md](agent.md) for the family overview and the `warded`
public face (`warded <role> <ref>`, the spelling these examples front). The roster is a
hard rename of the old verbs (ward#347): `work`/`headless`/`task` → `engineer`, `backlog` →
`director`, `reply`/`ask` → `advisor`, `sandbox` removed, and ward#353 folded the read-only
`explore`/`architect` into the **director's surface phase**. The old spellings error as
unknown commands.

A **bare ref with no role word runs `engineer`** (ward#282, ward#347): `warded #98`
dispatches the fire-and-forget engineer, and a bare `#N`/`N` infers `owner/repo` from the cwd
origin. The role words below override that default.

## The three roles

The canonical flat enumeration of the roles - one row each, with the tagline and the
ref-vs-freeform invocation modes - lives in **[agent-roster.md](agent-roster.md)**,
generated from the code roster by `ward agent roster` so it can never go stale
(ward#348). That page is the one source of truth for *which* roles exist; this doc and the
per-role docs ([agent-engineer.md](agent-engineer.md), [agent-director.md](agent-director.md)
+ its [surface](agent-surface.md), [agent-advisor.md](agent-advisor.md)) carry the prose
detail behind each row. Run `warded roster` for the list live at the terminal.

The notes below are the behavioral detail the flat roster does not capture:

- **`engineer`** (was `headless` + `task`) - **detached only** (ward#356): a ref
  runs the agent in print mode (`claude -p` etc.) to completion and exits into the reaper;
  for claude it **streams live progress** to the container log. From a terminal it first runs
  a **pre-flight check** ([agent-preflight.md](agent-preflight.md)): a GO launches, a NO-GO
  comments and launches nothing. Its seed **closes with a "how it felt" comment** (ward#281)
  led by a `WARD-OUTCOME` line (ward#310). No attach surface (`work`/`--watch` retired);
  interactive work funnels to `director`. Freeform text files an issue first, then carries it:
  DIRECT for an explicit `owner/repo`, ROUTE for a freeform task with no repo (ward#164).
- **`director`** (was `backlog`) - an attached heartbeat: polls `WARD-OUTCOME`, an LLM
  one-shot picks which queued issues to dispatch under `--max-parallel`, and on drain surfaces
  a **read-only scope + dispatch session** (push credential revoked, reaper skips salvage;
  ward#293, ward#351, ward#353; [agent-surface.md](agent-surface.md)).
- **`advisor`** (was `reply` + `ask`) - the ref mode researches one-shot and posts the
  answer as a comment; freeform answers *inside* a fresh container and streams it inline.

## Pre-flight parity

The engineer runs the **same pre-flight** ([docs/agent-preflight.md](agent-preflight.md))
in both ref and freeform mode (ward#149): freeform files the issue first, then gives the
same GO / NO-GO read before detaching. A NO-GO comments on the just-filed issue and
launches nothing, leaving a real issue a human can pick up or re-dispatch with
`engineer ... --no-preflight`. It honors the same skips (`--print`, `--no-preflight`, no
terminal). ROUTE's live survey *is* its feasibility gate, so ROUTE skips the pre-flight.

## Reaper backstop

The reaper backstop salvages residual work if the agent crashes (it needs ward's jail
off in-container - the entrypoint exports `CLIGUARD_NO_SANDBOX=1`, cli-guard#153). The
happy path doesn't rely on it: the agent commits/merges/pushes itself per its doctrine,
finishing to a clean `main` push.

## See also

- [docs/agent-roster.md](agent-roster.md) - the generated flat list of every role.
- [docs/agent.md](agent.md) - the `ward agent` roster and usage.
- [docs/agent-engineer.md](agent-engineer.md) - what the engineer does step by step.
- [docs/agent-preflight.md](agent-preflight.md) - the detached GO/NO-GO pre-flight.
