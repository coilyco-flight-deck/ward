# ward agent

`ward agent <name> work <issue>` is the short verb over [`ward container`](container.md)
for the common case: take a Forgejo issue and put an agent on it end to end. One
line replaces the full `container up <repo> --mode <m> --branch <b>` stack plus a
hand-written prompt.

## Usage

```bash
ward agent claude work coilyco-flight-deck/ward#98
ward agent claude work https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/98
ward agent claude headless coilyco-flight-deck/ward#98          # detached, fire-and-forget
ward agent codex  work coilyco-flight-deck/ward#98 --print      # resolve + show the plan, run nothing
```

`<name>` is the agent/mode (`claude|codex|qwen|goose`, the same context ladder as
`container up --mode`). The issue ref is `owner/repo#N` or a full Forgejo issue
URL. Any appended query string (`?thing=stuff`) or hash fragment
(`#issuecomment-149`) is ignored, so a URL copied straight from the browser works
unedited.

## Topics

The surface is split across focused docs:

- [docs/agent-work.md](agent-work.md) - what `work` does (resolve, trust-gate,
  branch, launch), the seed prompt, and the per-run container name.
- [docs/agent-subcommands.md](agent-subcommands.md) - `work` vs `headless`, plus
  `task`, `reply`, `ask`, the agent-only commit suite, and the reaper backstop.
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
