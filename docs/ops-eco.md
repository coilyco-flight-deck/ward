---
doc_goal: Pin the design that moved the Eco pipeline out of ward's compiled Go into the `ward ops eco` guardfiles on cli-guard's transport-agnostic stepflow engine, and record the migration + live-fire that let the dissolution land.
---
# ward ops eco: dissolution design

The design ([ward#585](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/585)) for dissolving the Go Eco pipeline ([ward#582](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/582)) into `ward ops eco`, and the record of that dissolution landing ([coilysiren/inbox#158](https://forgejo.coilysiren.me/coilysiren/inbox/issues/158)). Eco is Kai-fleet-specific, so it belongs in an authored ops area, not the binary (the [ward#578](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/578) principle).

## Target (shipped)

- `ward eco test` became `ward ops eco {native,server} {test,promote,...}`, one operator area expressed as two guardfiles - no compiled `EcoExecutor` or state machine.
- The **target selector is the subtree**, a real two-OS split (Kai's steer on [infrastructure#461](https://forgejo.coilysiren.me/coilyco-flight-deck/infrastructure/issues/461)): **native** is her Windows-local test server (the `eco-native-*.ps1` scripts), **server** is the prod Linux kai-server over the declared execverb ssh hop (the `eco-server-*.sh` scripts). [infrastructure#356](https://github.com/coilysiren/infrastructure/issues/356) gates only the optional agent-on-tower path, not this axis.

## The engine gap (the real deliverable, now closed)

specverb's `action` engine had **no compensation** and was **HTTP-bound**; execverb owned the exec/ssh transports but had **zero sequencing**. The deliverable was a transport-agnostic step abstraction plus rollback/compensate and canary primitives, reusable by any guarded-rollback pipeline. It landed in cli-guard as `pkg/stepflow` + exec-dialect actions ([cli-guard#187](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/issues/187)).

## Migration record (completed 2026-07-03, [coilysiren/inbox#158](https://forgejo.coilysiren.me/coilysiren/inbox/issues/158))

1. **cli-guard** - stepflow engine + exec-dialect actions landed ([cli-guard#187](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/issues/187) + v0.71.0: `pkg/stepflow`, positional step args, captured exec runner, per-grant `bin` override, `canary_blind`).
2. **infrastructure** - server env wiring ([infrastructure#461](https://forgejo.coilysiren.me/coilyco-flight-deck/infrastructure/issues/461)) + the native `eco-native-*.ps1` set, staged apply ([ward#584](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/584)), await-healthy gates, boot-anchored windows.
3. **ward** - `ward-kdl.eco-{native,server}.guardfile.kdl` landed; the apply step ships the tested working copy on both targets.
4. **ward** - `eco*.go` retired after live-fire. See [eco-test.md](eco-test.md) for usage and what the drills proved.

## Validation: live-fired

Native: green gate, red gate (real `error CS`), green promote, and a garbage-assembly promote that auto-rolled-back to a verified-healthy pre-drill state - all through `ward ops eco native ...` on the tower. Server: markers validated against a real boot journal, ssh leaves drilled, restore + loud-unhealthy paths drilled against a throwaway. The one deliberately unfired step is a full promote restarting the LIVE server - reserved for Kai ([coilyco-gaming/eco-ops#30](https://github.com/coilyco-gaming/eco-ops/issues/30)).

## See also

- [eco-test.md](eco-test.md) - the `ward ops eco` usage doc (the dissolved verb's successor).
- [ward-kdl.md](ward-kdl.md) - the build-time authoring layer a guardfile compiles through.
- [ops-forgejo-admin.md](ops-forgejo-admin.md) - the existing spec + exec-transport graft on one tree.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
