# ward agent: subcommand surfaces

The `ward agent` verbs differ in how attached they are and what they leave
behind. See [docs/agent.md](agent.md) for the family overview and the `warded`
public face (`warded <surface> <ref>`, the spelling these examples front).

A **bare ref with no surface word runs `headless`** (ward#282): `warded #98`
dispatches the fire-and-forget carry, and a bare `#N` / `N` infers `owner/repo`
from the cwd's git origin. The surface words below override that default.

## work vs headless

- **`work`** (interactive) attaches the container to your terminal - you watch and
  can step in. `--detach` backgrounds it.
- **`headless`** is the bare-ref default and is fire-and-forget: it always detaches and runs the agent in
  print mode (`claude -p`, `codex exec <seed>` for codex, or `goose run -t <seed>`
  for goose), so it works to completion non-interactively and exits into the
  reaper. For claude it **streams live progress** (one line per tool call + the
  result, via stream-json) to the container log - `docker logs <name>` / `docker
  exec <name> ...`; codex and goose print their own progress to that log - so it
  isn't silent until done. (Interactive `goose work` opens a bare `goose session`
  without the seed, so headless is the goose surface; `codex work` opens a seeded TUI.)
  When dispatched from a terminal it first runs a **pre-flight check** (see
  [docs/agent-preflight.md](agent-preflight.md)) - fire-and-forget: a GO launches
  the run, a NO-GO comments on the issue and launches nothing, with no prompt to
  answer. Its seed also asks it to **close with a "how it felt" comment** (ward#281)
  led by a `WARD-OUTCOME` line (ward#310) - the only voice a fire-and-forget run
  leaves behind. `task` and ROUTE inherit it; interactive `work` omits it (a human
  is already watching).
- **`task`** files an issue from `--instructions` first, then runs the `headless`
  flow against it (carries to merge, `closes #N`). See [docs/agent-task.md](agent-task.md).
- **`reply`** researches an issue one-shot and posts the answer as a comment - no
  container, no code change. See [docs/agent-reply.md](agent-reply.md).
- **`ask`** answers a freeform question one-shot *inside* a fresh container (so the
  answer can lean on the repo clone and operating context) and streams it inline - no
  issue, no code change, no comment. See [docs/agent-ask.md](agent-ask.md).
- **`sandbox`** is a *live interactive* agent in a fresh container, no issue, no
  seed - writable, nothing assigned. See [docs/agent-sandbox.md](agent-sandbox.md).
- **`explore`** is the **read-only** `sandbox`: push credential revoked after the
  clone, reaper skips salvage (ward#293). See [docs/agent-explore.md](agent-explore.md).
- **`backlog`** is an *autonomous supervised loop*, not an issue-carrying surface:
  it dispatches queued headless-lane issues up to `--max-parallel`, polls their
  `WARD-OUTCOME` comments, and repeats until the lane drains (ward#346). See
  [docs/agent-backlog.md](agent-backlog.md).

`task` runs the **same pre-flight** ([docs/agent-preflight.md](agent-preflight.md))
as `headless` (ward#149): it files the issue first, then gives the same GO / NO-GO
read before detaching. A NO-GO comments on the just-filed issue and launches
nothing, leaving a real issue a human can pick up or re-dispatch with
`headless ... --no-preflight`. It honors the same skips (`--print`,
`--no-preflight`, no terminal).

## Reaper backstop

The reaper backstop salvages residual work if the agent crashes (it needs ward's
jail off in-container - the entrypoint exports `CLIGUARD_NO_SANDBOX=1`, cli-guard#153).
The happy path doesn't rely on it: the agent commits/merges/pushes itself per its
doctrine, finishing to a clean `main` push.

## See also

- [docs/agent.md](agent.md) - the `ward agent` verb family and usage.
- [docs/agent-work.md](agent-work.md) - what `work` does step by step.
- [docs/agent-preflight.md](agent-preflight.md) - the headless GO/NO-GO pre-flight.
