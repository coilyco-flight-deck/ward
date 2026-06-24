# ward agent ask

`ward agent ask <question>` is the inline question surface of
[`ward agent`](agent.md): spin up a fresh ephemeral container - the same
fresh-clone-plus-operating-context the carry surfaces get - run the agent **one-shot**
against your question, and stream the answer straight back to your terminal. Unlike
`work`/`headless`/`task` it changes no code and carries nothing to merge, and unlike
[`reply`](agent-reply.md) it touches no issue and posts no comment - the whole job is
one good answer you read inline.

It runs in a container (not a bare host one-shot) on purpose: the questions worth
asking this way are usually the ones only answerable *with* the container's context -
the repo clone and the operating doctrine the agent reads on boot.

## Usage

```bash
ward agent ask "how does the reaper back-stop residual work?"
ward agent ask "what would break if WARD_GITCACHE moved?" --repo coilyco-flight-deck/ward
ward agent ask "summarize the release flow" --driver goose
ward agent ask "trace the ask path" --print   # show the repo + prompt + docker plan, run nothing
```

The whole argument tail is the question. It is canonically one quoted argument, but
any trailing words are joined so an unquoted multi-word question still works.

## What it does

1. **Join + validate** the question (required).
2. **Resolve** the context repo: `--repo owner/repo`, else inferred from the cwd's
   git origin - the same target resolution the container bring-up uses.
3. **Trust-gate** the owner (primary-org set), since ask spins a bypassPermissions
   container and clones the repo - the same gate `work`/`task` apply.
4. **Spin up** a fresh ephemeral container, attached, that fresh-clones the repo and
   composes the operating context, then runs the agent one-shot on the question.
5. **Stream** the answer to your terminal as the agent produces it; the container
   exits when the answer is done and the [reaper](container-reap.md) sweeps it.

The seeded prompt is explicit that the agent is answering inline (clean output, no
preamble, no sign-off), that it is **read-only** (read the code, run read-only
commands, search - but never commit, push, or open anything), and that it has the
fresh clone to lean on.

## How the one-shot runs per mode

ask reuses the container's one-shot argv, attached rather than detached:

* **claude** - `claude -p <question>` plain (no `--output-format stream-json`), so the
  answer streams clean to the terminal instead of the headless progress log.
* **goose** - `goose run -t <question>`.
* **codex** - `codex exec <question>`.
* **qwen** - `opencode run <question>`.

The container exports `WARD_ASK=1`; the entrypoint treats headless and ask as one-shot
together and only diverges for claude (ask drops the stream-json wrapper). ask never
sets `WARD_HEADLESS` and makes no commits - there is nothing to commit.

## `--print`

Resolves the repo and renders the question, the seeded prompt, and the full docker
plan, then exits without pulling, cloning, or running anything. Safe with no docker
daemon.

## Other flags

`--repo`, `--image`, `--tag`, `--ward-source`, `--aws`, and `--no-pull` mirror
the [container](container.md) bring-up flags [`work`](agent.md) carries. ask is always attached and
ephemeral, so there is no `--detach`, no branch, and no reservation.

## See also

- [docs/agent.md](agent.md) - the `ward agent` umbrella.
- [docs/agent-reply.md](agent-reply.md) - `reply`, the issue-bound research sibling.
- [docs/container.md](container.md) - the ephemeral, fresh-clone-inside container model.
- [docs/container-reap.md](container-reap.md) - the reaper that sweeps the run on exit.
