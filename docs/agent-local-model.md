---
doc_goal: Give an operator a straight verdict on pointing ward's local harnesses at their own Ollama - supported on native Linux via host-net, not yet on Docker Desktop - with the localhost-inside-a-container trap made explicit and the exact reason the endpoint is unrepointable today tied to the embedded fleet manifest and the tracked config-override fix.
---
# ward agent: bring your own local model (Ollama)

Answers what the driver pages skip: **can I point ward's local harnesses at the
Ollama I already run, and what do I set?** Read the verdict before a launch.

## Verdict

- **Native Linux, Ollama on the host:** supported today, one flag, no repoint.
- **Docker Desktop (macOS/Windows), your own Ollama:** **not supported yet** - the
  baked-in endpoint cannot be repointed and the only wired route is Coily's tower.

## Defaults, and the localhost trap

Endpoint + model are **baked into the embedded fleet manifest**
([`cmd/ward/fleetassets/fleet.generated.kdl`](../cmd/ward/fleetassets/fleet.generated.kdl)),
read at in-container bootstrap:

- **opencode** - `qwen3-coder:30b` at `http://localhost:11434/v1`.
- **goose** - provider `ollama`, `qwen3-coder:30b`, `OLLAMA_HOST` resolved host-side
  from SSM (the Coily tower) or goose's built-in `http://localhost:11434`.

Inside a container `localhost` is **the container**, not your host, so on docker's
default bridge the baked-in `localhost:11434` reaches **nothing**. A route only works
when it makes `localhost:11434` (or a tower forwarder) land on a live Ollama.

## Native Linux: host-net shares your Ollama

With Ollama on the host at `localhost:11434`, one flag makes the default reach it:

```bash
warded --harness opencode #98 --tailnet --tailnet-mode host-net
```

`--tailnet-mode host-net` runs `--network=host` ([agent-host-net.md](agent-host-net.md)),
so the container shares the host netns and `localhost:11434` **is** the host's. goose
falls back to its built-in `localhost:11434` (no tower to resolve), same result.

Rough edges:

- **Model is pinned** to `qwen3-coder:30b` - `WARD_QWEN_MODEL`/`WARD_GOOSE_MODEL` are
  not threaded from the host (see below). Run `ollama pull qwen3-coder:30b` first.
- **`--tailnet` implies `--aws`** (mounts `~/.aws` read-only; harmless if empty).
- **A tailnet `WARNING:`** may print with no `tailscale0`, but the run still launches
  and the local dial still works - it warns about tailnet reach, not your Ollama.

Verify: `--print` renders the `docker run` plan and exits (confirm `--network=host`).
At launch ward TCP-probes the endpoint and aborts clean if down
([agent-local-harnesses.md](agent-local-harnesses.md)); `WARD_SMOKE_TEST_SKIP=1` bypasses.

## Docker Desktop: not your own Ollama yet

No BYO path today: the default is the container's own loopback (unrepointable),
`--network=host` joins the LinuxKit VM not your host
([agent-host-net.md](agent-host-net.md)), and the sidecar
([agent-ts-sidecar.md](agent-ts-sidecar.md)) forwards to Coily's tower via the
`mac-proxy` box (its preflight fails without it), not to your Ollama. So: use the
tower with `--tailnet`, wait on the fix, or run ward on native Linux.

## Why there is no BYO knob yet

- `WARD_OLLAMA_URL`, `WARD_QWEN_MODEL`, `WARD_GOOSE_MODEL`, `WARD_GOOSE_PROVIDER` are
  read **only at in-container bootstrap** and are **not** threaded from the host, and
  there is no `--env` passthrough - setting them on your host does nothing.
- `~/.ward/fleet.local.kdl` ([fleet-local.md](fleet-local.md)) **rejects** the
  embed-only `fleet` block, so the endpoint/model cannot be overridden there either.

So they change only by editing the embedded manifest and rebuilding.
**[ward#395](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/395)**
("Externalize infra topology hardcoded in ward") tracks moving the Ollama URL, port,
and model into env/config so a BYO endpoint is first-class.

## See also

- [agent-local-harnesses.md](agent-local-harnesses.md) - local harness index + the launch probe.
- [agent-goose.md](agent-goose.md) / [agent-opencode.md](agent-opencode.md) - the driver pages.
- [agent-host-net.md](agent-host-net.md) / [agent-ts-sidecar.md](agent-ts-sidecar.md) - the advanced Coily tower/tailnet routes.
- [fleet-local.md](fleet-local.md) - the operator-local layer that can't repoint the endpoint yet.
