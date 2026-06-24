# ward drive / warded

`ward drive <harness> "<prompt>"` is the canonical machinery behind the **warded
agent**: spin up a fresh, least-access, ephemeral container, fresh-clone the context
repo, run the named harness **one-shot** against your prompt, and stream the output
back to your terminal. Every command the agent issues is gated by
[cli-guard](architecture.md) policy and written to an append-only audit row. The
boundary is the product: a denied command is the gate working, not a failure to route
around.

`warded` is the **public face** - a thin `ward` symlink the multicall rewrite turns into
`ward drive <args>`, not a second code path (ward#247). `warded claude "..."` reads like
`sudo`/`firejail`: one token for "containment tool for agents", a noun a bare `run`
(colliding with `ward exec`) cannot carry.

## Usage

```bash
warded claude "summarize how the audit log is written"
ward drive claude "summarize how the audit log is written"   # same thing, canonical spelling
ward drive --repo coilyco-flight-deck/ward codex "what does exec_gate.go enforce?"
warded --print claude "trace the drive path"                 # show the plan, run nothing
```

The first bare token is the harness (`claude|codex|qwen|goose`); everything after it is
the prompt. It is canonically one quoted argument, but trailing words are joined so an
unquoted multi-word prompt still works.

## Flag boundary

The harness arg is the boundary (ward#248): **ward flags go before it, the prompt after
it is raw.** So `warded --print claude "..."` dry-runs, but a `--print` *inside* the
prompt stays prompt text - the harness is where ward stops reading flags, so a prompt
can hold `--repo`, `--print`, or any dash-token unharmed. Ward splices a `--` after the
harness so urfave/cli skips flag-parsing past it; an explicit `--` does the same
(`warded claude -- "--literal-prompt"`).

## What it does

1. **Split** the tail at the harness boundary (ward#248): the harness (first bare
   token, validated) and the prompt (everything after it, raw). Both are required.
2. **Resolve** the context repo: `--repo owner/repo`, else inferred from the cwd's git
   origin - the resolution `ward container up` and [`ask`](agent-ask.md) use.
3. **Trust-gate** the owner (primary-org set): drive spins a bypassPermissions container
   and clones the repo, so the same gate `work`/`ask` apply.
4. **Spin up** a fresh attached ephemeral container that fresh-clones the repo and runs
   the harness one-shot, **streaming** the output; the [reaper](container-reap.md)
   sweeps it on exit.

The seed names the boundary and tells the harness the run is one-shot - work needing a
branch and merge belongs to [`ward agent work`](agent.md), not here.

## Relationship to `ward agent`

drive is the raw "run this harness under ward" primitive the launch pitch leads with.
The [`ward agent`](agent.md) surfaces are the issue-oriented sugar - resolve a Forgejo
issue, reserve it, pre-flight, carry it to merge. drive does none of that: one-shot,
attached, ephemeral, reusing `ask`'s one-shot branch (`WARD_ASK=1`).

## Flags

`--print` renders the repo, prompt, seed, and docker plan, then exits without pulling,
cloning, or running. `--repo`, `--with-repo`, `--image`, `--tag`, `--ward-source`,
`--aws`, and `--no-pull` mirror [`container up`](container.md). All ward flags precede
the harness (see [Flag boundary](#flag-boundary)).

## Shipping the `warded` symlink

The container [entrypoint](container.md) symlinks `warded -> ward` next to the binary,
so the demo runs `warded claude "..."` with no extra step. On a host install the homebrew
formula provides it (`bin.install_symlink "ward" => "warded"`).

## See also

- [docs/architecture.md](architecture.md) - ward in three layers; `warded` is the product.
- [docs/agent.md](agent.md) - the `ward agent` umbrella for issue-oriented surfaces.
- [docs/container.md](container.md) - the ephemeral, fresh-clone-inside container model.
