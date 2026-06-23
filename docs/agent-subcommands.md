# ward agent: subcommand surfaces

The `ward agent` verbs differ in how attached they are and what they leave
behind. See [docs/agent.md](agent.md) for the family overview.

## work vs headless

- **`work`** (interactive) attaches the container to your terminal - you watch
  the agent and can step in. `--detach` backgrounds it.
- **`headless`** is fire-and-forget: it always detaches and runs the agent in
  print mode (`claude -p`, `codex exec <seed>` for codex, or `goose run -t <seed>`
  for goose), so it works to completion non-interactively and exits into the
  reaper. For claude it **streams live progress** (one line per tool call + the
  result, via stream-json) to the container log - `docker logs <name>` / `ward
  container exec`; codex and goose print their own progress to that log - so it
  isn't silent until done. (Interactive `goose work` opens a bare `goose session`;
  the seed prompt is not auto-delivered into a session yet, so headless is the
  goose surface. Interactive `codex work` opens a seeded `codex` TUI.)
  When dispatched from a terminal it first runs a **pre-flight check** (see
  [docs/agent-preflight.md](agent-preflight.md)) - fire-and-forget: a GO launches
  the run, a NO-GO comments on the issue and launches nothing, with no prompt to
  answer.
- **`task`** files an issue from `--instructions` first, then runs the `headless`
  flow against it (carries to merge, `closes #N`). See [docs/agent-task.md](agent-task.md).
- **`reply`** researches an issue one-shot and posts the answer as a comment - no
  container, no code change. See [docs/agent-reply.md](agent-reply.md).
- **`ask`** answers a freeform question one-shot *inside* a fresh container (so the
  answer can lean on the repo clone and operating context) and streams it inline - no
  issue, no code change, no comment. See [docs/agent-ask.md](agent-ask.md).

Both `headless` and `task` also install an [agent-only commit suite](agent-precommit.md)
(`closes-issue` + `conventional-commit`) that interactive `work` and humans never see.

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
