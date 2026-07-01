# agentspi: the agent-agnostic contract

`internal/agentspi` is ward's per-agent seam contract (ward#410, Phase 1 of
ward#401). It is a **types-only, behaviour-free** package: the `Agent` interface,
the optional capability interfaces core feature-tests, and the narrow value
types that cross the core-to-agent boundary. Phase 2 adds
`internal/agents/<name>/` packages implementing these interfaces plus a registry
core dispatches through, retiring the `switch e.Mode` scatter the design
(ward#401) measured across `container_bootstrap.go`, `container_compute.go`, and
`agent_signature.go`.

## Why a contract package

ward is one flat `package main` in `cmd/ward` with **unexported**
`Runner`/`bootstrapEnv`. A sub-package cannot reach those symbols, so the seam
needs its own narrow value types rather than passing the whole `Runner` across
the boundary, which would reintroduce the import cycle the split exists to
remove. `agentspi` imports only `pkg/attribution` (for `Signer`) and the
standard library, so any agent package can import it freely.

## The interfaces

* `Agent` - the always-needed core: `Name`, `Record`, `Signer`, `LaunchArgv`, `PreflightArgv`.
* `CredentialProvider` - host resolve + container write of a credential (claude, codex, goose).
* `ConfigComposer` - writes a provider/model config file in-container (codex, opencode, goose).
* `Installer` - self-installs a binary absent from the image (opencode).
* `OnboardingSeeder` - seeds first-run state to skip interactive gates (claude).
* `LaunchGate` - a pre-launch check that can abort the run (claude's auth smoke test).

An agent that does not do X omits the impl, so core writes `if c, ok :=
agent.(agentspi.Installer); ok { ... }` instead of a self-skipping guard clause.

## The value types

* `Manifest` - the inert data record `Record()` serves (binary, contextLevel, stream, auth-kind, argv, identity, model). Phase 2 feeds it from the agent-adapter manifest, a superset of today's fields plus the aos#306 identity/model data.
* `RunCtx` - the narrow in-container view, carved out of `bootstrapEnv`: `AgentHome`, `TargetName`, the setpriv ids, the one-shot posture, the model/effort knobs, the ollama URL, the seed argv, plus an `Exec` seam and a `Log` logger.
* `HostCtx` - the narrow launching-host view: `GOOS`, operator `Home`, an `Exec` seam, a `Log` logger.
* `EnvLine` - one resolved credential env-file entry (`KEY`, `Value`); core renders it into the private `--env-file`.

`Exec` is the subprocess seam (`Exec` + `Capture`); cli-guard's `*shell.Runner`
satisfies it, so core passes its runner straight in. `Logger` is the blog-style
stderr logger, satisfied by the entrypoint's `blog()`.

## Phase 1 scope

No call site dispatches through a registry yet. Core keeps its switches. The
carve is proved by `agentHostCtx`/`agentRunCtx` in
[`cmd/ward/agentspi_ctx.go`](../cmd/ward/agentspi_ctx.go), pinned by
`agentspi_ctx_test.go` so a later rename of a carried field breaks the build.

## See also

- [agent-adapter-manifest.md](agent-adapter-manifest.md) - the data manifest that becomes `Manifest`'s source.
- [container.md](container.md) - the container model the two-host seam lives in.
- [agent-attribution.md](agent-attribution.md) - the `Signer` the `Agent` interface returns.
