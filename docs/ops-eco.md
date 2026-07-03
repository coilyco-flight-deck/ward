---
doc_goal: Pin the design for moving the Eco pipeline out of ward's compiled Go into a `ward ops eco` guardfile on a new transport-agnostic rollback/canary primitive in cli-guard, and why dissolution trails a validated replacement.
---
# ward ops eco: dissolution design

The design ([ward#585](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/585)) for dissolving the Go Eco pipeline ([ward#582](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/582)) into `ward ops eco` - Kai-fleet-specific, so an authored ops area, not the binary ([ward#578](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/578) principle). Child issues carry the implementation; `ward eco test` stays live until live-fired.

## Target

- `ward eco {test}` becomes `ward ops eco {test,promote}`, one operator area as a guardfile - no compiled `EcoExecutor` or state machine.
- A `native` | `server` **target selector** picks the transport per step: `server` is `passthrough tailscale ssh` (an execverb primitive), `native` is local exec. See below.

## The native/server model

Eco is a **Steam game that runs on both Linux and Windows**. **Prod is the Linux kai-server** (systemd `eco-server.service` + `eco-server-*.sh`) over `server` (`passthrough tailscale ssh`); Kai runs a **Windows server locally for test** over `native` (local exec). So `native`|`server` is a real **native=Windows-local + server=Linux-ssh** split - the original design shape, per the corrected [infrastructure#461](https://github.com/coilysiren/infrastructure/issues/461). [infrastructure#356](https://github.com/coilysiren/infrastructure/issues/356) (an Ansible role converging the agent stack on Windows towers) gates only an **optional warded agent there**, not Eco on Windows nor Kai's local test - not a blocker here.

## The engine gap (the real deliverable)

specverb's `action` engine does multi-step sequences, `$step.field` threading, a `fail-when` exit, and a bounded `poll` loop (`http/specverb/action_call.go`). It cannot express Eco, for two reasons:

- **No compensation.** A mid-sequence failure aborts forward-only. No health-gated rollback, no canary that rolls back on mid-loop degradation (only end-of-run `fail-when`).
- **HTTP-bound.** Every step fires HTTP (`buildCallRequest`/`fireCapture`). No "fire a resolved step" abstraction exists for exec/ssh. execverb owns transports but has zero sequencing.

The deliverable: grow specverb with a **transport-agnostic step abstraction** plus `rollback` and canary primitives, reusable by any guarded-rollback pipeline, eco first. Lands in cli-guard (native issue).

## Migration order

1. **cli-guard** - the step abstraction + rollback + canary primitives, unit-tested against a fake runner (native issue).
2. **infrastructure** - `native`|`server` transport wiring over the `eco-server-*.sh` scripts: `server` ssh to Linux prod, `native` local exec on Windows test (native issue).
3. **ward** - author `ward-kdl.eco.guardfile.kdl` on the new primitives, folding in [ward#584](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/584) (apply-step) and log-marker tuning.
4. **ward** - once the guardfile is live-fired green, retire `eco*.go` and its tests. Dissolution trails a validated replacement.

## Validation: live-fire

Unit tests cannot prove the gate's markers match a real EcoServer boot, or that rollback recovers it. Both need live-fire against a real or throwaway Eco (native Windows-local + server Linux-ssh), Kai's owner-gated hardware step. Engine and infra layers unit-test headless; the eco consumer does not.

## See also

- [eco-test.md](eco-test.md) - the current `ward eco test` pipeline being dissolved.
- [ward-kdl.md](ward-kdl.md) - the build-time authoring layer a guardfile compiles through.
- [ops-forgejo-admin.md](ops-forgejo-admin.md) - the existing spec + exec-transport graft.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
