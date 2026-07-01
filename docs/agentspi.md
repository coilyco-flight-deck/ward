# agentspi: the agent-agnostic contract

`internal/agentspi` is ward's per-agent seam contract (ward#410, Phase 1 of
ward#401). It is a **types-only, behaviour-free** package: the `Agent` interface,
the optional capability interfaces core feature-tests, and the narrow value
types that cross the core-to-agent boundary. Phase 2 adds
`internal/agents/<name>/` packages implementing these interfaces plus a registry
core dispatches through, retiring the `switch e.Mode` scatter ward#401 measured
across the container and signature code.

## Why a contract package

ward is one flat `package main` in `cmd/ward` with **unexported**
`Runner`/`bootstrapEnv`. A sub-package cannot reach those symbols, so the seam
needs its own narrow value types rather than passing the whole `Runner` across
the boundary (which would reintroduce the import cycle the split removes).
`agentspi` imports only `pkg/attribution` and the stdlib, so any agent package
can import it freely.

## The interfaces

* `Agent` - the core: `Name`, `Record`, `Signer`, `LaunchArgv`, `PreflightArgv`.
* `CredentialProvider` - host resolve + container write of a credential (claude, codex).
* `ConfigComposer` - writes a provider/model config file in-container (codex, opencode, goose).
* `Installer` - self-installs a binary absent from the image (opencode).
* `OnboardingSeeder` - seeds first-run state to skip interactive gates (claude).
* `LaunchGate` - a pre-launch check that can abort the run (claude's smoke test).

An agent that does not do X omits the impl, so core writes `if c, ok :=
agent.(agentspi.Installer); ok { ... }` instead of a guard clause.

## The value types

* `Manifest` - the inert data record `Record()` serves (binary, contextLevel, stream, auth-kind, argv, identity, model). Phase 3 feeds it from the fleet manifest.
* `RunCtx` - the narrow in-container view carved out of `bootstrapEnv`: `AgentHome`, `TargetName`, the setpriv ids, the one-shot posture, the model/effort knobs, the ollama URL, the seed argv, plus `Exec` + `Log` seams.
* `HostCtx` - the narrow launching-host view: `GOOS`, operator `Home`, an `Exec` seam, a `Log` logger.
* `EnvLine` - one resolved credential entry (`KEY`, `Value`) core renders into the `--env-file`.

`Exec` is the subprocess seam (`*shell.Runner` satisfies it); `Logger` is the
blog-style stderr logger (`blog()`).

## Phase rollout (ward#401)

- **Phase 1 (ward#410)** carved the types + the `agentHostCtx`/`agentRunCtx` views
  in [`agentspi_ctx.go`](../cmd/ward/agentspi_ctx.go); no registry.
- **Phase 2 (ward#412)** landed `internal/agents/{claude,codex,opencode,goose}` +
  `registry.go`, each `Agent` implementing exactly its capabilities. Data is pure
  per-package; capability methods forward to closures core injects in
  [`agents_wire.go`](../cmd/ward/agents_wire.go) (`WriteCreds` -> `writeClaudeCreds`).
  No call site flips - the switches stay live.
- **Phase 3 (ward#418)** flips core control flow to the registry.
  [`runContainerBootstrap`](../cmd/ward/container_bootstrap.go) dispatches
  capabilities via `wireAgent(mode)` + feature-tests (each fires only for the
  modes that implement it), and the `agentBinary`/`contextLevel`/`hostPreflightArgv`
  /`agentSigner` data reads resolve through `lookupAgent(mode).Record()` (fleetconfig
  -sourced, ward#416). Switches stay unreferenced-but-live.
- **Phase 4** deletes the dead switches, `parseMode` last.

The contract test [`agents_registry_contract_test.go`](../cmd/ward/agents_registry_contract_test.go)
pins the registry to the live switches entry-for-entry (ward#152) at every step.
The **qwen -> opencode** untangle keeps `--mode qwen` a deprecated alias; the
signing persona stays "Qwen".

## See also

- [agent-adapter-manifest.md](agent-adapter-manifest.md) - the data manifest that becomes `Manifest`'s source.
- [container.md](container.md) - the container model the two-host seam lives in.
- [agent-attribution.md](agent-attribution.md) - the `Signer` the `Agent` interface returns.
