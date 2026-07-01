# ward agent goose

`goose` is the other local harness, with provider config seeded by ward.

## Capabilities

- Host credential channel: none.
- Container config: writes `~/.config/goose/config.yaml`.
- Config seed includes `GOOSE_PROVIDER`, `GOOSE_MODEL`, and `OLLAMA_HOST`.

## Config shape

Ward resolves the host Ollama endpoint and seeds it base64'd as
`WARD_GOOSE_OLLAMA_HOST_B64`, then composes the goose config from that plus the
provider and model overrides.

## Install stance

goose is image-baked from the launcher point of view. No self-install step.

## Launch dialect

- Host preflight: none.
- Headless: `goose run -t <seed>`.
- Interactive: `goose session` with the issue pasted in by hand.

## Smoke gate

None. The launch is local-model only, so the GO/NO-GO read bows out.

## See also

- [docs/agent-local-harnesses.md](agent-local-harnesses.md) - the local harness index.
- [docs/agent.md](agent.md) - the roster and roles vs harnesses split.
