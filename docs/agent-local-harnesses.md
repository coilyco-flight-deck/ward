# ward agent: local-model harnesses (qwen, goose)

The local-model harnesses run against an Ollama model rather than a cloud
provider login. Cloud harness credentials (claude, codex) are covered in
[docs/agent-credentials.md](agent-credentials.md).

## qwen

**qwen** (ward#187) is backed by [opencode](https://opencode.ai) talking to a
**local ollama** model, so it needs no host credential - nothing rides the
`--env-file`. Because the aos dev-base image does not bake opencode in yet, the
entrypoint **self-installs the standalone opencode binary at container start**
(best-effort, the same stance ward takes for itself; an `--image` with opencode
baked in short-circuits it) and writes `~/.config/opencode/opencode.json`
registering a local ollama provider with the default model pinned to
`ollama/$WARD_QWEN_MODEL` (default `qwen2.5-coder:latest`, reachable at
`$WARD_OLLAMA_URL`, default `http://localhost:11434/v1`). Headless runs
`opencode run <seed>` (opencode prints its own progress, so ward pipes nothing
through the stream-json filter); interactive `work` opens the seedless `opencode`
TUI (the issue is pasted in by hand, like goose). There is no host pre-flight
one-shot - the model lives in the container, not on the host - so the GO/NO-GO
read bows out and dispatch proceeds. qwen carries the **minimal** context tier.

## goose

**goose** (ward#186) needs a **model provider** bound or a launched goose can do no
work. Unlike opencode (qwen), which points at a local in-container ollama needing no
host input, goose's config is ward's to seed - the same shape as codex. The default
provider is the **tower Ollama over the tailnet**: ward resolves its endpoint
host-side from SSM (`/coilysiren/ollama/host`, the param the ollama guardfile uses)
and rides it into the container base64'd over the private `--env-file` as
`WARD_GOOSE_OLLAMA_HOST_B64`, never in argv/audit. The entrypoint's
`compose_goose_config` then writes `~/.config/goose/config.yaml` binding
`GOOSE_PROVIDER`, `GOOSE_MODEL`, and `OLLAMA_HOST`, and scrubs
`WARD_GOOSE_OLLAMA_HOST_B64` from the env once decoded so the tailnet endpoint
does not linger in the agent's environment (ward#357, the same cleanup the
claude/codex cred blobs get). An unresolved host (no aws on
the host) just leaves goose to its built-in default rather than failing the launch.
Provider and model are overridable via `WARD_GOOSE_PROVIDER` / `WARD_GOOSE_MODEL`
(default `ollama` / `qwen2.5`), so an operator can repoint goose at a cloud peer -
plus that peer's key - without a code change.

## See also

- [docs/agent-credentials.md](agent-credentials.md) - claude and codex (cloud logins).
- [docs/agent.md](agent.md) - the `ward agent` verb family and usage.
