---
doc_goal: Serve as the reference entrypoint that makes a reader grasp ward agent as the single guarded launch surface for the coding-agent execution layer - the warded face, the startup-role roster, the driver axis, and where each deeper surface lives - not a thin container wrapper.
---
# ward agent

`ward agent` is **ward's whole second half: the guarded execution layer for
coding agents**. Each invocation takes a Forgejo issue and drives a
subscription-authenticated coding CLI (claude, codex, ...) through it from issue
to merge inside a **fresh, least-access ephemeral [container](container.md)** -
reach bounded by repo-scoped credentials, cli-guard policy, and a durable
append-only audit trail, landing per-run via `--workflow`. This is the product
end that the dev-verb gate secures the workbench for. It is **the** single launch
surface: the hand-run `ward container up`/`exec`/`down`/`ls` verbs are retired.

## Prerequisites

A **live** run needs **Docker running** (each boots an ephemeral
[container](container.md)) and a reachable **Forgejo** instance. `--print` needs
neither. Full list: [first-run.md §1](first-run.md#1-prerequisites).

## The `warded` public face

`warded` is the user-facing command: a thin `ward` symlink the multicall rewrite turns
into `ward agent <args>`, so `warded #98` *is* `ward agent
#98`. Read "warded" as the protective circle - the deny-list and allowlisted verbs
bounding the agent's reach.

## The startup-role roster

The surface is a roster of **startup roles** - short nouns that read like a team.
The **argument type** keys the mode: a ref acts on an issue, freeform text files
or answers it.

- **`engineer`** (was `headless`+`task`) - implements a ticket end to end,
  **detached only**. [agent-engineer.md](agent-engineer.md).
- **`director`** (was `backlog`) - autonomous backlog supervisor: dispatches
  engineers, surfaces a read-only session on drain. Holds the **live-observe set**
  (aws + tailnet) so its container reaches the live kai-server for the read-only
  `ward ops eco observe` surface ([ward#547](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/547)). [agent-director.md](agent-director.md).
- **`advisor`** (was `reply`+`ask`) - answers, writes no code: a ref comments,
  freeform is interactive. The advisor role holds the **live-observe guardfile
  set** (the tailnet + `~/.aws`, per its `roles` entry in `ward-kdl.fleet.kdl`,
  [ward#578](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/578)), so research reaches the backend with no flag, and `--no-tailnet`
  keeps a rare run isolated. [agent-advisor.md](agent-advisor.md).

The standalone `architect`/`explore`/`sandbox` roles now error - folded
them into the director's [surface session](agent-surface.md). The `--harness`
axis lives under [Drivers](#drivers) below.

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

The role comes first (`--harness` picks the harness, default claude; see
[Drivers](#drivers)). **A bare ref with no role word runs the `engineer` role**.
The ref is `owner/repo#N`, a full Forgejo URL, or a bare `#N` inferring
`owner/repo` from the cwd's git origin; a query string or `#fragment` is ignored.

## Drivers

`--harness` picks the harness, one click to each public setup page (credentials,
install stance, launch dialect, gates), no `internal/` source:
[claude](agent-claude.md), [codex](agent-codex.md) (cloud), [goose](agent-goose.md),
[opencode](agent-opencode.md) (local Ollama). Full comparison:
[agent-drivers.md](agent-drivers.md).

The flag was born `--driver` and renamed to match what it always picked
([ward#660](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/660)):
`--driver` survives one release cycle as a hidden deprecated alias, and an
explicit `--harness` wins when both are set.

## Topics

Grouped by the surface you are reaching for.

**Start here**

- [first-run.md](first-run.md) - zero to a first `--print` dry run.

**Roles and drivers** (what runs, and which harness runs it)

- [agent-roster.md](agent-roster.md) - flat list of every role (`ward agent roster`).
- [agent-subcommands.md](agent-subcommands.md) - the three roles compared + the reaper.
- [agent-drivers.md](agent-drivers.md) - the harnesses (`--harness`) compared.
- [agent-surface.md](agent-surface.md) - the director's read-only surface.

**Landing and safety** (how a run is fenced and where it lands)

- [agent-workflow.md](agent-workflow.md) - `--workflow direct-main|pr|patch-only`, the run's landing policy.
- [dispatch-review.md](dispatch-review.md) - the in-container adversarial-review panel gating a diff before it lands.
- [agent-preflight.md](agent-preflight.md) - the detached GO/NO-GO pre-flight.
- [agent-reap.md](agent-reap.md) - the host-side idle-killer.
- [agent-trust-gate.md](agent-trust-gate.md) - the owner trust gate.
- [agent-wrong-repo.md](agent-wrong-repo.md) - the WRONG-REPO blind-fire.
- [agent-reservation.md](agent-reservation.md) - reservation, TTL, `--force`.

**Flags and the container model**

- [agent-flags.md](agent-flags.md) - launch flags and `--details`.
- [container.md](container.md) - the container model (ephemeral clone).
