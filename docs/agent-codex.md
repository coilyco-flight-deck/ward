# ward agent codex

`codex` is the cloud harness with the open sandbox posture.

## Capabilities

- Host credential channel: `WARD_CODEX_AUTH_B64` in the private env-file.
- Container cred file: `~/.codex/auth.json`.
- Config composer: writes `~/.codex/config.toml`.

## Config shape

The bootstrap writes:

- `approval_policy = "never"`
- `sandbox_mode = "danger-full-access"`
- default model / reasoning / verbosity knobs, overridable by `WARD_CODEX_*`

## Install stance

codex is image-baked from the launcher point of view. No self-install step.

## Launch dialect

- Host preflight: none.
- Headless: `codex exec <seed>`.
- Interactive: `codex <seed>`.

## Smoke gate

None today. codex dispatch proceeds without the host GO/NO-GO preflight.

## See also

- [docs/agent-credentials.md](agent-credentials.md) - the shared cloud credential channel.
- [docs/agent.md](agent.md) - the roster and roles vs harnesses split.
