---
doc_goal: Pin the design for moving the Eco pipeline out of ward's compiled Go into a `ward ops eco` guardfile on a new transport-agnostic rollback/canary primitive in cli-guard, and why dissolution trails a validated replacement.
---
# ward ops eco: dissolution design

The design ([ward#585](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/585)) for dissolving the Go Eco pipeline ([ward#582](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/582)) into `ward ops eco`. Eco is Kai-fleet-specific, so it belongs in an authored ops area, not the binary (the [ward#578](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/578) principle). The child issues below carry the implementation, and `ward eco test` stays live until its replacement is live-fired.

## Target

- `ward eco {test}` becomes `ward ops eco {test,promote}`, one operator area expressed as a guardfile - no compiled `EcoExecutor` or state machine.
- A `native` | `server` **target selector** picks the transport per step - a transport axis on **one Linux Eco**, not an OS split. `server` is `passthrough tailscale ssh` to kai-server (an execverb primitive), `native` is local exec on the host.

## The native premise correction

The issue frames `native` as a local **Windows** box with a per-OS op divergence. The repo has no such thing: Eco is Linux-only on kai-server (systemd `eco-server.service` + the `eco-server-*.sh` scripts), and "native" in the eco runbooks is the non-k3s LAN process, not Windows. The Windows tower is blocked upstream on [infrastructure#356](https://github.com/coilysiren/infrastructure/issues/356), so `native` scopes to **local Linux exec**, Windows deferred behind it.

## The engine gap (the real deliverable)

specverb's `action` engine already does multi-step sequences, `$step.field` threading, a post-hoc `fail-when` exit, and a bounded `poll` loop (`http/specverb/action_call.go`). It cannot express Eco, for two reasons:

- **No compensation.** A mid-sequence failure aborts forward-only ("nothing after it ran"). No health-gated rollback / compensating step, no canary that rolls back on mid-loop degradation (only end-of-run `fail-when`).
- **HTTP-bound.** Every step fires HTTP (`buildCallRequest`/`fireCapture`). No "fire a resolved step" abstraction exists for an exec/ssh transport. execverb owns transports but has zero sequencing.

The deliverable: grow specverb with a **transport-agnostic step abstraction** plus `rollback`/compensate and canary primitives, reusable by any guarded-rollback pipeline, eco the first consumer. Lands in cli-guard, its own native issue.

## Migration order

1. **cli-guard** - the step abstraction + rollback + canary primitives, unit-tested against a fake step runner (native issue).
2. **infrastructure** - `native`|`server` transport wiring over the `eco-server-*.sh` scripts (native issue, Windows deferred behind the blocked tower foundation).
3. **ward** - author `ward-kdl.eco.guardfile.kdl` on the new primitives, folding in [ward#584](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/584) (the apply-step) and log-marker tuning.
4. **ward** - once the guardfile is live-fired green, retire `eco*.go` and its tests. Dissolution trails a validated replacement.

## Validation: live-fire

Unit tests cannot prove the gate's markers match a real EcoServer boot, or that rollback recovers the target. Both need live-fire against a real or throwaway Eco (native and server) - Kai's owner-gated hardware step. The engine and infra layers are unit-testable headless, the eco consumer is not.

## See also

- [eco-test.md](eco-test.md) - the current `ward eco test` pipeline being dissolved.
- [ward-kdl.md](ward-kdl.md) - the build-time authoring layer a guardfile compiles through.
- [ops-forgejo-admin.md](ops-forgejo-admin.md) - the existing spec + exec-transport graft on one tree.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
