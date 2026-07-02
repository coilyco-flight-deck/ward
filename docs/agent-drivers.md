# ward agent drivers

`--driver` picks which harness carries the issue inside the container
([agent.md](agent.md)). Two families: **cloud** harnesses authenticate to a
hosted model with a host credential ward seeds in, and **local** harnesses drive
an Ollama-backed model over a reachable endpoint with no credential channel. Read
your driver's page before a first run - this page lays the first-run facts side by
side. Running your own Ollama? [agent-local-model.md](agent-local-model.md) is the
bring-your-own-Ollama page: what works today and what does not ([#395](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/395)).

## claude (cloud, default)

- Page: [agent-claude.md](agent-claude.md).
- Credential: Max/subscription OAuth login resolved on the host, seeded into the container's `~/.claude/.credentials.json` ([agent-credentials.md](agent-credentials.md)). `ANTHROPIC_API_KEY` stays unset.
- Install: image-baked, no self-install.
- Gates: host GO/NO-GO [pre-flight](agent-preflight.md) **and** a bounded `claude -p` auth smoke gate that aborts on a dead credential.

## codex (cloud)

- Page: [agent-codex.md](agent-codex.md).
- Credential: host `~/.codex/auth.json` (`codex login` - ChatGPT login or API key), seeded into the container ([agent-credentials.md](agent-credentials.md)). An absent file leaves codex unauthenticated.
- Install: image-baked, no self-install.
- Gates: none - no host pre-flight and no smoke gate, dispatch proceeds. Cheapest posture by default, overridable via `WARD_CODEX_*`.

## goose (local Ollama)

- Page: [agent-goose.md](agent-goose.md).
- Credential: none. ward seeds the resolved Ollama endpoint into `~/.config/goose/config.yaml` ([agent-local-harnesses.md](agent-local-harnesses.md)).
- Install: image-baked, no self-install.
- Gates: **no** host [pre-flight](agent-preflight.md) - as a local-model harness goose is barred from the unsandboxed host read ([ward#162](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/162)); it detaches straight into its container, gated only by the in-container Ollama reachability probe.

## opencode (local Ollama)

- Page: [agent-opencode.md](agent-opencode.md).
- Credential: none. Config binds `WARD_OLLAMA_URL` ([agent-local-harnesses.md](agent-local-harnesses.md)), the renamed `qwen` mode.
- Install: best-effort self-install of the `opencode` binary at container start.
- Gates: no host pre-flight, but the same in-container Ollama reachability probe.

## See also

- [agent.md](agent.md) - the `ward agent` verb family and the Drivers pointer.
- [agent-credentials.md](agent-credentials.md) - the shared cloud credential channel.
- [agent-local-harnesses.md](agent-local-harnesses.md) - the local harness index and Ollama probe.
- [agent-local-model.md](agent-local-model.md) - bring your own Ollama: defaults, the supported route, and the current limitation ([#395](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/395)).
