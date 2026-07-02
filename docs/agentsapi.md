# agentsapi: the agent-agnostic contract

`internal/agentsapi` is ward's per-agent seam contract (renamed from
`agentspi`). It is a **types-only,
behaviour-free** package: the `Agent` interface, the optional capability
interfaces core feature-tests, and the narrow value types crossing the
core-to-agent boundary. The `internal/agents/<name>/` packages implement these
and **own their per-agent behaviour** (creds, config, install, onboarding, the
launch gate); core dispatches through the registry, retiring the `switch e.Mode`
scatter.

## Why a contract package

ward is one flat `package main` in `cmd/ward` with **unexported**
`Runner`/`bootstrapEnv`. A sub-package cannot reach those symbols, so the seam
needs its own narrow value types rather than passing the whole `Runner` across
the boundary. `agentsapi` imports only `pkg/attribution` and the stdlib, so any
agent package can import it freely.

## The interfaces

* `Agent` - the core: `Name`, `Record`, `Signer`, `LaunchArgv`, `PreflightArgv`.
* `CredentialProvider` - host resolve + container write of a credential (claude, codex).
* `ConfigComposer` - writes a provider/model config file in-container (codex, opencode, goose).
* `Installer` - self-installs a binary absent from the image (opencode).
* `OnboardingSeeder` - seeds first-run state to skip interactive gates (claude).
* `LaunchGate` - a pre-launch check that aborts the run (claude auth; goose/opencode ollama reach).

An agent that does not do X omits the impl, so core feature-tests
`if c, ok := agent.(agentsapi.Installer); ok { ... }` instead of a guard.

## The value types

* `Manifest` - the inert data record `Record()` serves (binary, contextLevel, stream, auth-kind, argv, identity, model), fed from the fleet manifest.
* `RunCtx` - the narrow in-container view carved from `bootstrapEnv`: `AgentHome`, `TargetName`, setpriv ids, one-shot posture, model/effort knobs, ollama URL, seed argv, plus `Exec` + `Log` seams.
* `HostCtx` - the narrow launching-host view: `GOOS`, operator `Home`, `Exec`, `Log`.
* `EnvLine` - one resolved credential entry (`KEY`, `Value`) core renders into the `--env-file`.

`Exec` is the subprocess seam (`*shell.Runner` satisfies it); `Logger` is the
blog-style stderr logger (`blog()`).

## Phase rollout

- **Phase 1** carved the types + the `agentHostCtx`/`agentRunCtx` views
  in `agentsapi_ctx.go`; no registry.
- **Phase 2** landed `internal/agents/{claude,codex,opencode,goose}` +
  `registry.go`. Data was pure per-package; capability methods forwarded to
  closures core injected in `agents_wire.go`, switches still live.
- **Phase 3** drained every per-agent body **home**: the closures are
  gone, and each folder's capability methods run real code against
  `RunCtx`/`HostCtx`. [`runContainerBootstrap`](../cmd/ward/container_bootstrap.go)
  resolves via `lookupAgent(mode)` and dispatches by feature-test through
  `composeAgentContainer`; host-side `resolveAgentCreds` routes through the
  `CredentialProvider` seam. This phase renamed `agentspi` -> `agentsapi`.
- **Phase 4** deletes the still-live mode/argv/signer switches (the mode->binary
  table in `container_compute.go`, the roster identity in `agent_signature.go`),
  `parseMode` last.

The contract test `agents_registry_contract_test.go` pins the registry to the
switches. The drain ratchet `agent_drain_gate_test.go` fails if a
per-agent name reappears as a string literal in a core file outside that tiny
allowlist. The **qwen -> opencode** untangle keeps `--mode qwen` a deprecated
alias; the signing persona stays "Qwen".

## See also

- [agent-adapter-manifest.md](agent-adapter-manifest.md) - the data manifest that becomes `Manifest`'s source.
- [container.md](container.md) - the container model the two-host seam lives in.
- [agent-attribution.md](agent-attribution.md) - the `Signer` the `Agent` interface returns.
