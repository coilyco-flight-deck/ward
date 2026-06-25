# ward agent engineer

`ward agent engineer` (public face `warded engineer`) is the **implement-a-ticket**
role of the startup roster (ward#347): it carries a Forgejo issue end to end -
implement, commit, merge to main, push, `closes #N`. It merges the retired
`work`/`headless`/`task` verbs, and **the argument type selects the mode**. See
[docs/agent.md](agent.md) for the roster and the `warded` face.

A **bare ref with no role word also routes to engineer** (ward#282, ward#347), so
`warded #98` *is* `warded engineer #98` - the fire-and-forget default.

## Usage

```bash
warded engineer coilyco-flight-deck/ward#98     # ref -> detached carry (was headless)
warded engineer #98 --watch                     # interactive: attach and pair (-w; was work)
warded engineer "fix the flaky exec_gate test"  # freeform -> file an issue, then carry (was task)
warded engineer coilyco-flight-deck/ward -i "add a --foo flag"   # freeform DIRECT
warded #98                                      # bare ref -> the engineer carry (default)
```

## Argument-type dispatch

The first argument decides the mode (`parseAgentIssueRef` succeeds → ref mode; it
errors on non-ref text → freeform):

- **A ref** (`owner/repo#N`, a bare `#N` / `N` inferring `owner/repo` from the cwd's
  git origin, or a Forgejo issue URL) carries that existing issue.
- **Freeform text** (or a bare `owner/repo` plus `--instructions`) files an issue
  first, then carries it (the retired `task` flow).

## Ref mode: detached vs `--watch`

It validates the ref (a bad ref or untrusted owner fails before any container spins),
branches `issue-<N>` (override `--branch`), and launches a fresh-clone `ward container`
seeded to carry the issue. The owner is trust-gated (primary-org set; bypassPermissions).

By default the carry **detaches** fire-and-forget (was `headless`): print mode
(`claude -p`, `codex exec`, `goose run -t`), streaming progress to the container log
for claude. From a terminal it first runs a **pre-flight** ([agent-preflight.md](agent-preflight.md)):
a GO launches, a NO-GO comments and launches nothing. Its seed asks it to close with a
`WARD-OUTCOME`-led retrospective (ward#281, ward#310) the [director](agent-director.md)
loop reads. `--watch` (`-w`) flips it to **interactive, attached** (was `work`); the
`--new-tab` spawn ([agent-flags.md](agent-flags.md)) rides with it. The seed's first
move is shaped by body and harness (ward#157): empty bodies say so, non-vision
harnesses get the body inlined with media stripped.

## Freeform mode (was `task`)

When the argument is not a ref, engineer files an issue, carries it, and closes it.
Two sub-modes by repo omission:

- **DIRECT** — an explicit `owner/repo` (or `--instructions`/`-i` with cwd inference);
  filed there and carried, same detached carry + pre-flight. Title is the first
  instruction line (≤72 runes); body is the instructions + a provenance footer.
- **ROUTE** (ward#164) — a freeform task and no repo. ward files an intake record in
  `coilysiren/inbox`, surveys the fleet one-shot to route it (`REPO` / `UNCLEAR`),
  files a scoped child, cross-links + closes intake, then carries the child. An
  UNCLEAR or untrusted target bounces to a human. The survey *is* the gate (ROUTE
  skips the pre-flight); it needs a claude/goose host slot, else use DIRECT (ward#148).

Pass exactly one of `--instructions`/`-i` or `--instructions-file`.

## Trust gate and dry-run

The trust gate runs before anything is filed (in ROUTE the routed target too).
`--print` files/runs nothing: a ref renders the seed + plan; freeform renders the
issue that would be filed (`#N` shows `#0`); ROUTE renders the intake + live flow.

## See also

- [docs/agent.md](agent.md) - the roster and the `warded` face.
- [docs/agent-subcommands.md](agent-subcommands.md) - the roles compared.
- [docs/agent-preflight.md](agent-preflight.md) - the detached pre-flight.
- [docs/agent-flags.md](agent-flags.md) - launch flags, `--details`, `--new-tab`.
