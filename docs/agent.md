# ward agent

`ward agent` is **the** entrypoint to the ephemeral [container](container.md)
subsystem: take a Forgejo issue and put an agent on it end to end. It shares the
container bring-up Go directly (it does not shell out). ward#263 retired the old
hand-run `ward container up` / `exec` / `down` / `ls` verbs, so `ward agent` is
now the single launch surface; one line replaces a full bring-up stack plus a
hand-written prompt.

## The `warded` public face

`warded` is the product's user-facing command: a thin `ward` symlink the
multicall rewrite turns into `ward agent <args>`, not a second code path
(ward#247, ward#282). So `warded #98` *is* `ward agent #98` - the ergonomic daily
driver for the dispatcher Kai runs dozens of times a day. Read "warded" as a
protective circle: the deny-list and allowlisted verbs bounding the agent's
reach. Everything below applies to both spellings.

## Usage

```bash
warded coilyco-flight-deck/ward#98          # bare ref -> headless (the fire-and-forget default)
warded #98                                  # owner/repo inferred from the cwd's git origin
warded work #98                             # interactive: attach and watch
warded headless #98 --driver codex          # pick another harness
warded task "fix the flaky exec_gate test"  # file the issue, then carry it
warded ask "how is the audit log written?"
warded reply #98
warded sandbox                              # interactive agent, fresh clone, no issue
warded explore                              # like sandbox, but read-only (cannot push)
ward agent work coilyco-flight-deck/ward#98 # the canonical spelling warded fronts
```

The surface (`work|headless|task|reply|ask|sandbox|explore`) comes first; `--driver` picks the
agent/mode (`claude|codex|qwen|goose`, default `claude`, the container context
ladder). ward#185 moved the harness off a subcommand slot onto `--driver`,
leaving room for a future `--reviewer` role flag.

**A bare ref with no surface word runs the `headless` carry** (ward#282) - the
fire-and-forget default, since by the time the issue exists the design is done
and the PR is the review gate. The issue ref is `owner/repo#N`, a full Forgejo
issue URL, or a **bare `#N` / `N`** that infers `owner/repo` from the cwd's git
origin (so `warded #98` from inside the ward checkout means
`coilyco-flight-deck/ward#98`, mirroring how `ask` already infers its context
repo). Any appended query string (`?thing=stuff`) or hash fragment
(`#issuecomment-149`) is ignored, so a URL copied straight from the browser works
unedited.

## Topics

The surface is split across focused docs:

- [docs/agent-work.md](agent-work.md) - what `work` does (resolve, trust-gate,
  branch, launch), the seed prompt, and the per-run container name.
- [docs/agent-subcommands.md](agent-subcommands.md) - `work` vs `headless`, plus
  `task`, `reply`, `ask`, `sandbox`, `explore`, and the reaper backstop.
- [docs/agent-sandbox.md](agent-sandbox.md) - `sandbox`, the unguided interactive
  session (fresh clone, no issue, no seed).
- [docs/agent-explore.md](agent-explore.md) - `explore`, the read-only sibling of `sandbox`.
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
