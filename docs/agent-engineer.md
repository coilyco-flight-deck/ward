# ward agent engineer

`ward agent engineer` (public face `warded engineer`) is the **implement-a-ticket**
role of the startup roster (ward#347): it carries a Forgejo issue end to end -
implement, commit, merge to main, push, `closes #N`. It folds in the retired
`headless`/`task` verbs, and **the argument type selects the mode**. Engineer is
**detached / autonomous only** (ward#356): hands-on work goes to the
[director](agent-director.md). See [docs/agent.md](agent.md) for the roster.

A **bare ref with no role word also routes to engineer** (ward#282, ward#347), so
`warded #98` *is* `warded engineer #98` - the fire-and-forget default.

## Usage

```bash
warded engineer coilyco-flight-deck/ward#98     # ref -> detached carry
warded engineer "fix the flaky exec_gate test"  # freeform -> file an issue, then carry
warded engineer coilyco-flight-deck/ward -i "add a --foo flag"   # freeform DIRECT
warded #98                                      # bare ref -> the engineer carry (default)
```

## Argument-type dispatch

The first argument decides the mode (`parseAgentIssueRef` succeeds → ref mode; it
errors on non-ref text → freeform):

- **A ref** (`owner/repo#N`, a bare `#N` / `N` inferring `owner/repo` from the cwd's
  git origin, or a Forgejo issue URL) carries that existing issue.
- **Freeform text** (or a bare `owner/repo` plus `--instructions-file`) files an issue
  first, then carries it (the retired `task` flow).

## Ref mode: the detached carry

It validates the ref (a bad ref or untrusted owner fails before any container spins),
branches `issue-<N>` (override `--branch`), and launches a fresh-clone `ward container`
seeded to carry the issue. The owner is trust-gated (primary-org set; bypassPermissions).

The carry always **detaches** fire-and-forget (was `headless`): print mode
(`claude -p`, `codex exec`, `goose run -t`). From a terminal it first runs a
**pre-flight** ([agent-preflight.md](agent-preflight.md)):
a GO launches, a NO-GO comments and launches nothing. Its seed closes with a
`WARD-OUTCOME`-led retro (ward#281, ward#310) the [director](agent-director.md) reads,
and is shaped by body + harness (ward#157, ward#400): empty bodies say so, **every**
driver gets the body inlined as a **frozen snapshot** at dispatch (non-vision
media-stripped, vision verbatim, URL kept for comments/images), and the whole seed is
**logged at dispatch**, greppable without `--print`.

There is **no attach surface** (ward#356): the old `work`/`--watch` + `--new-tab` Warp
spawn (ward#174) are retired; interactive work funnels to the [director](agent-director.md).

## Freeform mode (was `task`)

When the argument is not a ref, engineer files an issue, carries it, and closes it.
Two sub-modes by repo omission:

- **DIRECT** — an explicit `owner/repo` (or `--instructions-file` with cwd inference; the
  inline `--instructions`/`-i` was retired in ward#362); filed there and carried, same
  detached carry + pre-flight. Title is the first instruction line; body is the
  instructions + a provenance footer.
- **ROUTE** (ward#164) — a freeform task and no repo. ward files an intake record in
  `coilysiren/inbox`, surveys the fleet to route it (`REPO`/`UNCLEAR`), files a scoped
  child, cross-links + closes intake, then carries the child. An
  UNCLEAR or untrusted target bounces to a human. The survey *is* the gate (ROUTE
  skips the pre-flight); it needs a claude/goose host slot, else use DIRECT (ward#148).

## Trust gate and dry-run

The trust gate runs before anything is filed (in ROUTE the routed target too).
`--print` files/runs nothing: a ref renders the seed + plan; freeform renders the
issue that would be filed; ROUTE renders the intake + live flow.

## See also

- [docs/agent.md](agent.md) - the roster and the `warded` face.
- [docs/agent-subcommands.md](agent-subcommands.md) - the roles compared.
- [docs/agent-preflight.md](agent-preflight.md) - the detached pre-flight.
- [docs/agent-flags.md](agent-flags.md) - launch flags and `--details`.
