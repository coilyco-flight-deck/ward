---
doc_goal: Convey the claude harness as ward's primary full cloud driver and give an operator the concrete capabilities - credential seeding, onboarding pre-trust, and the launch smoke gate - needed to reason about a claude-driven run.
---
# ward agent claude

`claude` is ward's **default `--driver` and its only fully-wired harness** - the
whole guarded agent flow is built around it. Every role (engineer, director,
advisor) runs on claude unless `--driver` overrides it, and claude alone gets the
full host-side wiring the other harnesses lack: the one-shot `claude -p` launch
preflight, the `stream-json` headless channel, and the advisor ref mode. It
implements host credential seeding, container credential writing, onboarding
seeding, and that launch gate.

## Why non-root, subscription login

claude is the harness that bounds its own reach. It runs **non-root** (uid 1000,
the entrypoint sets up as root then drops via `setpriv`) because it refuses
`--dangerously-skip-permissions` as root, and it authenticates with your
**Max/subscription OAuth login**, not an `ANTHROPIC_API_KEY` (which stays unset so
it cannot shadow the OAuth token). That posture is the point: a subscription
credential seeded into a non-root least-access box is what keeps a claude run
inside the container's blast radius instead of carrying a broad API key around.
Full credential path: [agent-credentials.md](agent-credentials.md).

## Capabilities

- Host credential channel: `WARD_CLAUDE_CREDS_B64` in the private env-file.
- Container cred file: `~/.claude/.credentials.json`.
- Onboarding seed: `~/.claude.json` - skips the first-run wizard and pre-trusts
  every directory the agent may cd into (the target clone, the `/workspace` root,
  each granted extra repo, and every warmed `/substrate` reference repo), so an
  interactive or director session never re-hits the folder-trust dialog
  ([ward#168](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/168)).
- Smoke gate: the bounded `claude -p` auth probe before launch.

## Config shape

claude writes no extra model config file. The container relies on the seeded
credential plus the onboarding state file.

**Model / effort ([ward#616](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/616)).**
The `agent claude` fleet node carries empty `model` / `reasoning-effort` defaults, so a
run launches bare `claude` (no `--model`) exactly as before. `WARD_CLAUDE_MODEL` (or
`--config agent.claude.model=<v>`) appends `--model <v>` at launch; `WARD_CLAUDE_REASONING_EFFORT`
(`--config agent.claude.effort=<v>`) is carried through to the startup config echo, but claude
has **no native reasoning-effort flag** today, so it is not applied to the launch argv. Both
resolve highest-first `--config` > `WARD_*` env > fleet default. See [agent-flags.md](agent-flags.md).

## Install stance

claude is image-baked. No self-install step.

## Launch dialect

- Host preflight: `claude -p <prompt>`.
- Headless: `claude -p --verbose --output-format stream-json <seed>`.
- Interactive: `claude <seed>` plus the seedless TUI flow.
- A resolved `WARD_CLAUDE_MODEL` appends `--model <v>` to any of the three ([ward#616](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/616)).

## Smoke gate

The auth probe runs as the agent user, times out if it hangs, and aborts the run
on empty output or timeout. It also checks for the Docker-disk stall case before
blaming auth, so a full disk does not look like a bad login. `WARD_SMOKE_TEST_SKIP=1`
bypasses it.

## See also

- [docs/agent-credentials.md](agent-credentials.md) - the shared cloud credential channel.
- [docs/agent.md](agent.md) - the roster and roles vs harnesses split.
