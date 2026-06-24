# ward agent

`ward agent work <issue>` is **the** entrypoint to the ephemeral
[container](container.md) subsystem for the common case: take a Forgejo issue and
put an agent on it end to end. It shares the container bring-up Go directly (it
does not shell out). ward#263 retired the old hand-run `ward container up` /
`exec` / `down` / `ls` verbs, so `ward agent` is now the single launch surface;
one line replaces a full bring-up stack plus a hand-written prompt.

## Usage

```bash
ward agent work coilyco-flight-deck/ward#98                          # --driver defaults to claude
ward agent work https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/98
ward agent headless coilyco-flight-deck/ward#98                      # detached, fire-and-forget
ward agent work coilyco-flight-deck/ward#98 --driver codex --print   # pick a harness; show the plan, run nothing
```

The surface (`work|headless|task|reply|ask`) comes first; `--driver` picks the
agent/mode (`claude|codex|qwen|goose`, default `claude`, the container context
ladder). ward#185 moved the harness off a subcommand slot onto
`--driver`, leaving room for a future `--reviewer` role flag. The issue ref is
`owner/repo#N` or a full Forgejo issue URL. Any appended query string
(`?thing=stuff`) or hash fragment (`#issuecomment-149`) is ignored, so a URL
copied straight from the browser works unedited.

## Topics

The surface is split across focused docs:

- [docs/agent-work.md](agent-work.md) - what `work` does (resolve, trust-gate,
  branch, launch), the seed prompt, and the per-run container name.
- [docs/agent-subcommands.md](agent-subcommands.md) - `work` vs `headless`, plus
  `task`, `reply`, `ask`, and the reaper backstop.
- [docs/agent-preflight.md](agent-preflight.md) - the headless GO/NO-GO
  pre-flight and when it is skipped.
- [docs/agent-wrong-repo.md](agent-wrong-repo.md) - the WRONG-REPO blind-fire
  routing path.
- [docs/agent-credentials.md](agent-credentials.md) - how claude and codex
  credentials are seeded into the container.
- [docs/agent-local-harnesses.md](agent-local-harnesses.md) - the local-model
  harnesses (qwen, goose) and their Ollama config.
- [docs/agent-reservation.md](agent-reservation.md) - issue reservation, TTL,
  `--force` reclaim, and the host stale-ward reminder.
- [docs/agent-flags.md](agent-flags.md) - launch flags, `--details`, and the
  `--new-tab` sidequest spawn.

## See also

- [docs/container.md](container.md) - the container model this wraps (ephemeral,
  fresh-clone-inside, least-access, reaper-backed).
