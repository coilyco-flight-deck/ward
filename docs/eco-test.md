---
doc_goal: Make a reader understand `ward eco test` as the Eco content pipeline - a fail-closed local smoke gate that, only on green and only when asked, drives a snapshot + health + canary + auto-rollback promote to the live kai-server - so they know what runs locally, what touches the live world, and where safety lives.
---
# ward eco test

`ward eco test` is the Eco content pipeline ([ward#582](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/582), [coilyco-gaming/eco-ops#30](https://github.com/coilyco-gaming/eco-ops/issues/30)). It boots a **throwaway** EcoServer with your working-copy mod/config change on the local Steam install, runs a **fail-closed smoke gate**, and - only on a green gate and only with `--promote` - drives a guarded auto-promote to the live Linux kai-server. The live world only ever ends **green or reverted**.

## The two halves

- **Local gate (Windows-native).** Boots a disposable world from the local Steam `EcoServer` install with the working-copy change staged in, captures the boot log, runs the smoke gate. Safe anytime; touches no live server.
- **Guarded promote (opt-in).** On a green gate with `--promote`, wraps the existing promote pieces (`install-eco-mod.sh`, the systemd restart) in a snapshot + health-check + canary + auto-rollback envelope against kai-server.

## Usage

```bash
ward eco test --mod EcoTelemetry              # gate the change, no promote
ward eco test --mod EcoTelemetry --promote    # gate, then auto-promote on green
ward eco test --promote --dry-run             # print the plan, run nothing
```

`--promote` is **off by default**: the gate is the everyday surface, and the first real auto-promote against the live server is Kai's ([coilyco-gaming/eco-ops#30](https://github.com/coilyco-gaming/eco-ops/issues/30)). `--dry-run` prints the plan and touches nothing.

## The smoke gate (fail-closed)

The gate (`analyzeSmoke` in `cmd/ward/eco_gate.go`) blocks the promote unless **all** hold: the server reached a **ready** marker (silence is not success), **no ModKit load exception** ([coilyco-gaming/eco-ops#7](https://github.com/coilyco-gaming/eco-ops/issues/7)), **no UserCode compile failure**, and every `--mod` reported **loaded**. Reaching ready is itself evidence UserCode compiled, since EcoServer aborts startup on a compile error.

## The promote envelope (only if the gate is green)

Driven by `runPromote` in `cmd/ward/eco_promote.go` over ssh, using the infrastructure-repo scripts `eco-server-{snapshot,health-check,rollback}.sh`:

1. **Snapshot first** - a snapshot failure aborts **without touching** the live server.
2. **Apply** the mod (reuses `install-eco-mod.sh`).
3. **Restart** `eco-server.service`.
4. **Probe** health: unit active, journal clean of ModKit exceptions, ready ([coilyco-gaming/eco-ops#26](https://github.com/coilyco-gaming/eco-ops/issues/26)).
5. **Canary watch** - re-probe `--canary-samples` times every `--canary-interval`, rolling back on the **first** degradation.
6. Healthy through the window -> **promoted**. Any failure -> restore the snapshot + restart.

The one outcome that stops for a human is a rollback that **restores but does not come back healthy**: `ward eco test` exits with a distinct code and a MANUAL INTERVENTION message.

## Configuration

Env/flag driven so it stays at the operator-local layer, out of the repocfg schema: `WARD_ECO_SERVER_DIR`/`--server-dir` (local install), `WARD_ECO_KAI_HOST`/`--kai-host` (ssh target), `WARD_ECO_REMOTE_DIR`, `WARD_ECO_SCRIPTS_DIR`, `WARD_ECO_SNAPSHOT_ROOT`, `WARD_ECO_RESTART_CMD`.

## Where the safety logic lives

The safety-critical logic is **pure and unit-tested** in ward (`eco_gate.go`, `eco_promote.go`); the side effects are a thin seam (`eco_run.go`: local boot + the ssh-backed `EcoExecutor`). The live-side steps are the infrastructure-repo scripts, in the `eco-server-setup` skill's promote-rollback runbook.

## See also

- [docs/FEATURES.md](FEATURES.md) - the shipping inventory.
