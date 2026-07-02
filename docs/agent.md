# ward agent

`ward agent` is **the** entrypoint to the ephemeral [container](container.md)
subsystem: take a Forgejo issue and put an agent on it end to end. ward#263
retired the hand-run `ward container up`/`exec`/`down`/`ls` verbs, so `ward agent`
is the single launch surface.

## The `warded` public face

`warded` is the user-facing command: a thin `ward` symlink the multicall rewrite turns
into `ward agent <args>` (ward#247, ward#282), so `warded #98` *is* `ward agent
#98`. Read "warded" as the protective circle - the deny-list and allowlisted verbs
bounding the agent's reach.

## The startup-role roster (ward#347, ward#353)

The surface is a roster of **startup roles** - short nouns that read like a team.
The **argument type** keys the mode: a ref acts on an issue, freeform text files
or answers it.

- **`engineer`** (was `headless`+`task`) - implements a ticket end to end,
  **detached only** (ward#356). [agent-engineer.md](agent-engineer.md).
- **`director`** (was `backlog`) - autonomous backlog supervisor: dispatches
  engineers, surfaces a read-only session on drain. [agent-director.md](agent-director.md).
- **`advisor`** (was `reply`+`ask`) - answers, writes no code: a ref comments,
  freeform is interactive (ward#388). [agent-advisor.md](agent-advisor.md).

ward#353 folded the standalone `architect`/`explore` role into the director's
[surface session](agent-surface.md); `warded architect`/`explore`/`sandbox` now
error.

The startup roles are the **roles axis**. The `--driver` harness axis and its
per-driver setup pages live under [Drivers](#drivers) below. `internal/agents/<name>`
is the Go adapter for each driver - a maintainer implementation detail, not the
page a user or agent reads to set a driver up.

## Usage

```bash
warded coilyco-flight-deck/ward#98              # bare ref -> engineer run (fire-and-forget)
warded #98                                      # owner/repo inferred from the cwd's git origin
warded engineer #98                             # implement a ticket: detached fire-and-forget
warded engineer "fix the flaky exec_gate test"  # freeform -> file an issue first, then carry
warded director --repo owner/name               # autonomous headless-lane loop; surfaces a read-only session on drain
warded advisor #98 "what would it take to..."   # research the issue, post a comment
warded advisor "how is the audit log written?"  # freeform: interactive (--oneshot = one answer)
```

The role comes first (`--driver` picks the harness, default claude; ward#185; see
[Drivers](#drivers)). **A bare ref with no role word runs the `engineer` role**
(ward#347). The ref is `owner/repo#N`, a full Forgejo URL, or a bare `#N` inferring
`owner/repo` from the cwd's git origin; a query string or `#fragment` is ignored.

## Drivers

`--driver` picks the harness, one click to each public setup page (credentials,
install stance, launch dialect, gates), no `internal/` source:
[claude](agent-claude.md), [codex](agent-codex.md) (cloud), [goose](agent-goose.md),
[opencode](agent-opencode.md) (local Ollama). Full comparison:
[agent-drivers.md](agent-drivers.md).

## Topics

- [first-run.md](first-run.md) - **start here if new**: zero to a first `--print` dry run.
- [agent-roster.md](agent-roster.md) - flat list of every role (`ward agent roster`).
- [agent-subcommands.md](agent-subcommands.md) - the three roles compared + the reaper.
- [agent-drivers.md](agent-drivers.md) - the four `--driver` harnesses compared.
- [agent-surface.md](agent-surface.md) - the director's read-only surface.
- [agent-preflight.md](agent-preflight.md) - the detached GO/NO-GO pre-flight.
- [agent-trust-gate.md](agent-trust-gate.md) - the owner trust gate.
- [agent-wrong-repo.md](agent-wrong-repo.md) - the WRONG-REPO blind-fire path.
- [agent-reservation.md](agent-reservation.md) - reservation, TTL, `--force`.
- [agent-flags.md](agent-flags.md) - launch flags and `--details`.
- [container.md](container.md) - the container model (ephemeral clone).
