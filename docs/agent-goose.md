---
doc_goal: Make an operator confident running the goose local-Ollama harness at parity with claude - how ward seeds its provider config, why a local-model harness is barred from the host pre-flight and gated instead by the in-container Ollama reachability probe, and what the bring-your-own-Ollama limits are today.
---
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

**Bring your own Ollama:** the endpoint here is resolved **host-side from SSM** (the
Coily tower) or falls back to goose's built-in `http://localhost:11434`, and it is
**not** user-overridable from the host today. If you run your own Ollama, read
[agent-local-model.md](agent-local-model.md) before launching: on native Linux the
host-net route shares your local Ollama, but Docker Desktop and repointing the
endpoint are not supported yet ([#395](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/395)).

## Install stance

goose ships in the dev-base image and launches today - no self-install step. The
launcher's one drop-to-shell fallback fires only when an agent binary is **absent**
from the image (in practice just `opencode`, if its self-install fails). goose
is baked in, so `--driver goose` launches the harness rather than dropping to a
shell. It is a first-class option at parity with claude, not an afterthought.

## Launch dialect

- Host preflight: **none** - goose is a local-model harness, so it is **barred
  from the unsandboxed host GO/NO-GO read** ([ward#162](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/162)) and detaches straight into
  its isolated container run. The host read runs the agent with full host tool
  access, and a weak local model that ignores the read-only framing would act on
  the host's real checkouts before the container starts; only a trusted cloud
  harness (claude) keeps it. See [agent-preflight.md](agent-preflight.md).
- Headless: `goose run -t <seed>` (inside the container, against the fresh clone).
- Interactive: `goose session` with the issue pasted in by hand.

## Smoke gate

Two gates sit at different points, and only one applies here:

- **Host GO/NO-GO pre-flight** (pre-dispatch, [agent-preflight.md](agent-preflight.md)): **does not run for goose** - as a local-model harness it is barred from the unsandboxed host read ([ward#162](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/162)) and detaches straight into its container. The pre-flight is a claude-only (trusted-cloud) gate now.
- **In-container Ollama reachability probe** (pre-launch, [ward#487](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/487)): the local-model analog of claude's auth smoke test. A headless goose whose Ollama endpoint is down would hang the dispatched container exactly like an undetected bad claude credential (the failure mode a smoke gate exists to prevent). So before launching, the entrypoint TCP-probes the endpoint goose will dial - the `OLLAMA_HOST` seeded into `config.yaml`, or goose's built-in `http://localhost:11434` when no tower host resolved - with a short retry window (absorbing the `--ts-sidecar` forwarder's startup). On an unreachable endpoint it **aborts the container with a clear error** naming the endpoint and how to recover (a live tower host, or `--ts-sidecar`), instead of letting it silently hang. The probe is headless-only (an interactive session has a human watching); set `WARD_SMOKE_TEST_SKIP=1` to bypass it (the same switch claude's probe reads).

## See also

- [docs/agent-local-model.md](agent-local-model.md) - bring your own Ollama: defaults, the supported route, and the current limitation ([#395](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/395)).
- [docs/agent-local-harnesses.md](agent-local-harnesses.md) - the local harness index.
- [docs/agent.md](agent.md) - the roster and roles vs harnesses split.
