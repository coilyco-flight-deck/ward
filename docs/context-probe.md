# Role-aware three-tier context probe + per-driver spec (design)

**Status: design-first ([ward#373](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/373)).** Proposes an
architecture and a schema, commits neither. The full proposal (accounting detail,
trade-offs) is in the issue thread; the open forks below are Kai's to settle.

## Why

A low-capability driver (opencode/qwen, any small local model) fails not only when
context is **missing** but when it is **too much**. Three tiers load a driver at
init, each drowning a weak model differently (init load, not runtime sweep):

- **Proactive** - eager prompt bytes: composed `AGENTS.container.md` + level-gated
  `CLAUDE.md`/`AGENTS.md` + role overlays (read-only block, front-load seed,
  statusline, issue-corpus index) + skill frontmatter.
- **Immediate** - the `/workspace/<name>` working clone: a real grep surface, one
  tool call away, not in the prompt.
- **Peripheral** - the `/substrate/<name>` reference repos (8 today): cheap to
  provide, a bigger haystack to pick wrongly from.

## Gap over what exists

`check_context_budget` (agentic-os, `ward context-budget`) measures the proactive
axis **per harness**, not per **role**, and misses the container-only overlays and
tiers 2/3. `context-level 2|1|0` in the fleet manifest is the only per-driver knob:
one int, no per-tier budget, no enforcement. (The `agent-adapters.yaml` the issue
names is retired; the live source is the fleet KDL.)

## Probe architecture

**Static-compose by default, real-spin as an opt-in verifier.** The proactive tier
is deterministic from inputs ward owns (embedded `AGENTS.container.md`, the level
ladder, the overlay strings), so a host-side probe reproduces it with no Docker;
tiers 2/3 are a `git ls-files` walk of the clone and the substrate mirrors. A
`--spin` flag boots one real container per (role, driver) cell and diffs measured
against predicted, catching composition drift. Reuse ward's own compose functions,
so drift surfaces as a test failure.

## Per-driver spec (proposed, not locked)

Extend the fleet manifest's `context-level` int with an additive per-tier budget
block. **First schema instance - it sets the pattern, so it is proposed for Kai,
not committed.** An absent block = unbudgeted (today's behavior, backward-compatible).
Candidate KDL shape:

```kdl
agent opencode {
    context-level 0                          // kept: the compose ladder reads it
    context-budget {
        proactive  tokens=5000               // eager prompt ceiling
        immediate  files=1500 tokens=400000  // working-clone ceiling
        peripheral repos=3 tokens=600000     // substrate ceiling
    }
}
```

## Authoring-vs-rollout split (aos doctrine)

- **Measurement leg in agentic-os** - extend `check_context_budget` with the
  working-dir + substrate walkers (tracked-file count / bytes / token proxy),
  measurement-only, reversible. Cross-filed as an agentic-os companion.
- **Role-spin probe + spec schema in ward** - ward owns roles, containers,
  substrate, `WARD_CONTEXT_LEVEL`. The probe calls the aos walker for tiers 2/3
  and reuses its doc/skill accounting for tier 1, then adds the container overlays.

## Open forks for Kai

1. **Spec schema shape** - units per tier (tokens only, or +file-count), whether
   `peripheral` gates the manifest, per-driver vs per-(driver, role).
2. **Real-spin vs static** - recommendation is static + `--spin`; real-spin-only
   trades speed for fidelity.
3. **Where `--check` lives** - ward verb, aos hook, or split (respecting the
   authoring law: aos cannot reach up into ward's role model).
4. **Token meaning for tiers 2/3** - a 3k-file repo is millions of chars; file
   count + bytes may signal more honestly than a token proxy implying full ingest.

## See also

- [container.md](container.md) - the container model + compose ladder.
- [agent-adapter-manifest.md](agent-adapter-manifest.md) - the fleet manifest carrying `context-level`.
