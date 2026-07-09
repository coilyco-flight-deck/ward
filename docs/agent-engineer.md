---
doc_goal: Make an operator fluent in the engineer role as the detached, issue-to-merge worker of the guarded execution layer - how the argument type selects ref vs freeform mode, why the run is fire-and-forget with a frozen seed and an immutable reserved issue, and where the reaper gate lands it.
---
# ward agent engineer

`ward agent engineer` (public face `warded engineer`) is the **implement-a-ticket**
role of the startup roster: it carries a Forgejo issue end to end -
implement, commit, merge to main, push, `closes #N`. It folds in the retired
`headless`/`task` verbs, and **the argument type selects the mode**. Engineer is
**detached / autonomous only**: hands-on work goes to the
[director](agent-director.md).

A **bare ref with no role word also routes to engineer**, so
`warded #98` **is** `warded engineer #98` - the fire-and-forget default.

## Usage

```bash
warded engineer coilyco-flight-deck/ward#98     # ref -> detached run
warded engineer "fix the flaky exec_gate test"  # freeform -> file, then carry
warded engineer coilyco-flight-deck/ward -i "add a --foo flag"   # freeform DIRECT
warded #98                                      # bare ref -> the engineer (default)
```

## Argument-type dispatch

The first argument decides the mode (`parseAgentIssueRef` succeeds for a ref, and
errors on non-ref text for freeform):

- **A ref** (`owner/repo#N`, a bare `#N` / `N` inferring `owner/repo` from the cwd's
  git origin, or a Forgejo issue URL) carries that issue.
- **Freeform text** (or a bare `owner/repo` plus `--instructions-file`) files an issue
  first, then carries it (retired `task` flow).

## Ref mode: the detached run

It validates the ref (a bad ref or [untrusted owner](agent-trust-gate.md) trips
first), branches `issue-<N>` (override `--branch`), and launches a clone.
Reaper enforces `closes #N` before landing, including on the already-landed fast
path.

The engineer always **detaches** fire-and-forget (was `headless`): print mode
(`claude -p`/`codex exec`/`goose run -t`). From a terminal it first runs a **pre-flight**
([agent-preflight.md](agent-preflight.md)): a GO launches, a NO-GO comments and launches
nothing. Its seed closes with a `WARD-OUTCOME`-led retro the
[director](agent-director.md) reads, and now asks for the review summary or skip state
inside that same final comment's collapsed details. The only visible text is the
one-line outcome. For `pull-requests` runs, the seed also tells the worker to keep
watching the PR checks after opening the PR, fix failures, and report
`WARD-OUTCOME: submitted` at the PR boundary instead of `done`. For
`pull-requests-and-merge` runs, the engineer reports `WARD-OUTCOME: merge-ready`
and leaves the final `done` for the director after merge. The seed is shaped by
the body:
empty bodies say so, **every** driver gets the body inlined **verbatim** as a **frozen
snapshot** at dispatch (image markup intact, a no-vision line, URL live for
comments), **logged** for `grep`.

Because that snapshot is never re-read, a **reserved issue is immutable** to the run in
flight - a correction after dispatch goes to a new issue, not an edit or comment: see
[reserved means immutable](agent-reserved-immutable.md).

**Stopping the engineer.** `docker container stop engineer-<driver>-<repo>-<issue>` halts a
mis-scoped one - see [container-stop.md](container-stop.md) for the reaper interaction.

## Freeform mode (was `task`)

When the argument is not a ref, engineer files an issue, carries it, closes it.
Two sub-modes by repo omission:

- **DIRECT** - an explicit `owner/repo` (or `--instructions-file` with cwd inference),
  filed there and carried, same detached run + pre-flight. Title is the first
  instruction line (≤72 runes), body is the instructions + a provenance footer.
- **ROUTE** - a freeform task and no repo. ward files an intake record in
  `coilysiren/inbox`, surveys the fleet to route it (`REPO`/`UNCLEAR`), files a scoped
  child, cross-links + closes intake, then carries the child. An UNCLEAR or untrusted
  target bounces to a human. The survey **is** the gate (ROUTE skips the pre-flight), it
  needs a trusted cloud host slot, else use DIRECT.

## Trust gate and dry-run

The trust gate runs before anything is filed (in ROUTE the routed target too).
`--print` files/runs nothing: a ref renders the seed + plan; freeform renders the
filed issue; ROUTE renders the intake + live flow.

## See also

- [docs/agent.md](agent.md) - the roster and the `warded` face.
- [docs/agent-subcommands.md](agent-subcommands.md) - the roles compared.
- [docs/agent-preflight.md](agent-preflight.md) - the detached pre-flight.
- [docs/agent-flags.md](agent-flags.md) - launch flags and `--details`.
