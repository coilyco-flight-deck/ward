# ward agent sandbox

`ward agent sandbox` is the unguided interactive surface of
[`ward agent`](agent.md): spin up a fresh ephemeral container - the same
fresh-clone-plus-operating-context the carry surfaces get - and drop you into a
**live, interactive agent** with **no issue and no seed**. It is the interactive
sibling of [`ask`](agent-ask.md): same container, but instead of a one-shot answer
to a question you get a full agent session to drive yourself.

Every other surface seeds a prompt - `work`/`headless` a Forgejo issue, `task` an
issue it files first, `reply`/`ask` a question. `sandbox` seeds nothing: it is the
bare interactive bring-up the retired `ward container up` used to be (ward#263),
now back under the agent umbrella where the credentials and trust gate live.

It is **writable**, not read-only: the run gets the same push token a `work` carry
gets, so the agent can commit, merge, and push - it is a full scratch dev box in a
throwaway clone, just with nothing assigned. Use it to poke at the repo with an
agent, try a spike, or hand-drive work that has no issue yet.

## Usage

```bash
warded sandbox                                    # claude in a fresh clone of the cwd's repo
ward agent sandbox --repo coilyco-flight-deck/ward
ward agent sandbox --driver codex                 # pick another harness
ward agent sandbox --repo coilyco-flight-deck/ward --print   # show the docker plan, run nothing
```

It takes no positional argument - there is no ref and no question to pass.

## What it does

1. **Resolve** the context repo: `--repo owner/repo`, else inferred from the cwd's
   git origin - the same target resolution `ask` and the container bring-up use.
2. **Trust-gate** the owner (primary-org set), since sandbox spins a
   bypassPermissions container with a push token and clones the repo - the same
   gate `work`/`task`/`ask` apply.
3. **Spin up** a fresh ephemeral container, attached, that fresh-clones the repo and
   composes the operating context, then launches the agent with no seed.
4. **Drop you in**: you drive the agent interactively until you exit, then the
   container exits and the [reaper](container-reap.md) sweeps it.

## How the interactive session runs per mode

sandbox is the bare interactive bring-up - empty agent argv, so the entrypoint
launches each harness in its plain interactive mode:

* **claude** - `claude` (no `-p`, no seed).
* **goose** - `goose session`.
* **codex** - `codex`.
* **qwen** - `opencode`.

The container sets neither `WARD_ASK` nor `WARD_HEADLESS`, so the entrypoint takes
the interactive branch. There is no seed prompt to deliver.

## `--print`

Resolves the repo and renders the full docker plan, then exits without pulling,
cloning, or running anything. There is no seed to show. Safe with no docker daemon.

## Other flags

`--repo`, `--with-repo`, `--image`, `--tag`, `--ward-source`, `--aws`, and
`--no-pull` mirror the [container](container.md) bring-up flags the carry surfaces
carry. sandbox is always attached and ephemeral, so there is no `--detach` (a
seedless detached agent would just sit idle), no branch, and no reservation (there
is no issue to reserve).

## See also

- [docs/agent.md](agent.md) - the `ward agent` umbrella.
- [docs/agent-ask.md](agent-ask.md) - `ask`, the one-shot question sibling.
- [docs/container.md](container.md) - the ephemeral, fresh-clone-inside container model.
- [docs/container-reap.md](container-reap.md) - the reaper that sweeps the run on exit.
