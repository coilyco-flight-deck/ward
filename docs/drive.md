# ward drive / warded

`ward drive <harness> "<prompt>"` is the canonical machinery behind the **warded
agent**: spin up a fresh, least-access, ephemeral container - the same
fresh-clone-plus-operating-context the [`ward agent`](agent.md) surfaces get - run the
named harness **one-shot** against your prompt, and stream the output back to your
terminal. Every command the agent issues is gated by [cli-guard](architecture.md) policy
and written to an append-only audit row. The boundary is the product: a denied command
is the gate working, not a failure to route around.

`warded` is the **public face** - a thin symlink to the `ward` binary that the multicall
rewrite turns into `ward drive <args>`, not a second code path (ward#247). `warded
claude "..."` reads like `sudo`, `timeout`, `nice`, `firejail`: one token an SRE parses
as "containment tool for agents", instantiating the warded-agent noun a bare `run`
cannot carry. `run` was rejected for colliding with `ward exec`.

## Usage

```bash
warded claude "summarize how the audit log is written"
ward drive claude "summarize how the audit log is written"   # same thing, canonical spelling
warded codex "what does exec_gate.go enforce?" --repo coilyco-flight-deck/ward
warded claude "trace the drive path" --print                 # show the plan, run nothing
```

The first positional is the harness (`claude|codex|qwen|goose`); everything after it is
the prompt. It is canonically one quoted argument, but trailing words are joined so an
unquoted multi-word prompt still works.

## What it does

1. **Split** the tail into the harness (first token, validated) and the prompt (the
   rest, joined). Both are required.
2. **Resolve** the context repo: `--repo owner/repo`, else inferred from the cwd's git
   origin - the resolution `ward container up` and [`ask`](agent-ask.md) use.
3. **Trust-gate** the owner (primary-org set): drive spins a bypassPermissions container
   and clones the repo, so the same gate `work`/`ask` apply.
4. **Spin up** a fresh attached ephemeral container that fresh-clones the repo and runs
   the harness one-shot, **streaming** the output; the [reaper](container-reap.md)
   sweeps it on exit.

The seed names the boundary: the agent is a warded agent in a guarded, ephemeral
container where every command is cli-guard-gated and audited, the run is one-shot, and
work needing a branch and merge belongs to [`ward agent work`](agent.md), not here.

## Relationship to `ward agent`

drive is the raw "run this harness under ward" primitive the launch pitch leads with.
The [`ward agent`](agent.md) surfaces (`work`, `headless`, `task`, `reply`, `ask`) are
the issue-oriented sugar - they resolve a Forgejo issue, reserve it, pre-flight, and
carry it to merge. drive does none of that: one-shot, attached, ephemeral. It reuses
`ask`'s one-shot attached branch (`WARD_ASK=1`); the seed, not the env, frames the run,
and the container is named `ward-<repo>-drive-<mode>-<rand>`.

## Flags

`--print` renders the repo, prompt, seed, and docker plan, then exits without pulling,
cloning, or running (safe with no docker daemon). `--repo`, `--with-repo`, `--image`,
`--tag`, `--ward-source`, `--aws`, and `--no-pull` mirror [`container up`](container.md).
drive is always attached and ephemeral - no `--detach`, no branch, no reservation.

## Shipping the `warded` symlink

The container [entrypoint](container.md) symlinks `warded -> ward` next to the binary,
so the demo runs `warded claude "..."` with no extra step. On a host install the homebrew
formula provides it (`bin.install_symlink "ward" => "warded"` in the tap's
`Formula/ward.rb`); the multicall rewrite does the rest either way.

## See also

- [docs/architecture.md](architecture.md) - ward in three layers; `warded` is the product.
- [docs/agent.md](agent.md) - the `ward agent` umbrella for issue-oriented surfaces.
- [docs/container.md](container.md) - the ephemeral, fresh-clone-inside container model.
