---
doc_goal: Make a reader operate `ward ops eco {native,server}` - the guardfile-driven Eco content pipeline that replaced the compiled `ward eco test` - knowing what runs where, what the promote envelope guarantees, and which live-fire proved it.
---
# ward ops eco

The Eco content pipeline is the exec-dialect guardfile area `ward ops eco`
([ward#585](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/585), [coilysiren/inbox#158](https://forgejo.coilysiren.me/coilysiren/inbox/issues/158)). The compiled `ward eco test` verb
([ward#582](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/582)) is dissolved: orchestration now lives in
`cmd/ward-kdl/ward-kdl.eco-{native,server}.guardfile.kdl`, run by cli-guard's
stepflow engine, driving the infrastructure-repo scripts. The target selector
is the subtree: **native** is the local Windows test install, **server** is the
prod Linux kai-server over the declared ssh hop.

## Usage

```bash
ward ops eco native test <src> [mods-csv]   # smoke-gate the working copy (throwaway world)
ward ops eco native promote <src>           # guarded promote to the LOCAL test server
ward ops eco server promote <src>           # guarded promote to the LIVE kai-server
ward ops eco {native,server} {snapshot,health,await,rollback,...}   # the step verbs
ward ops eco server promote --dry-run <src> # print the plan, run nothing
```

`<src>` is the working-copy root holding `Mods/` (and optionally `Configs/`) -
the exact tree the gate tested is what ships ([ward#584](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/584)), never a released
asset. The gate boots `-offline` with a throwaway world; it never presents an
identity to Strange Cloud ([coilyco-gaming/eco-ops#30](https://github.com/coilyco-gaming/eco-ops/issues/30)).

## The promote envelope

snapshot (compensation registered) -> apply the tested copy -> restart ->
await-healthy (bounded; Eco boots take minutes) -> canary watch (20s x 60s
window). Any step failure or canary degradation fires the compensations in
reverse: the snapshot is restored, the server restarted, and recovery
VERIFIED - a restore that does not come back healthy exits loud
(`action_failed`, manual intervention). The target only ever ends green or
reverted. An unobservable canary also rolls back (`canary_blind`).

## What live-fire proved (2026-07-03, the tower + kai-server)

- The ready marker matches a real EcoServer 0.13 boot on both targets
  (`Server Initialization ... Finished`); the five prior guesses did not.
- The red gate fail-closes on real `error CS` lines - and Eco 0.13 boots to
  ready DESPITE UserCode compile errors, so the gate is the only stop.
- A garbage-assembly promote died mid-boot, await failed fast, and the
  auto-rollback restored the exact pre-drill state, verified healthy.
- The server-side restore + loud-unhealthy paths drilled over ssh against a
  throwaway; the full live-server promote is deliberately reserved for Kai.

## The read-only observe sibling

`ward ops eco observe` is the **read-only** third subtree ([ward#547](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/547)):
a non-mutating window on the live kai-server (`status`, `logs`, `mods`, `configs`,
`read-config`) for the warded director, split from this `server` promote surface so
an observer never touches the write path. Full walkthrough: [eco-observe.md](eco-observe.md).

## See also

- [ops-eco.md](ops-eco.md) - the dissolution design and migration record.
- [eco-observe.md](eco-observe.md) - the read-only observe sibling.
- [docs/FEATURES.md](FEATURES.md) - the shipping inventory.
