# ward agent claude

`claude` is the full cloud harness. It implements host credential seeding,
container credential writing, onboarding seeding, and a launch gate.

## Capabilities

- Host credential channel: `WARD_CLAUDE_CREDS_B64` in the private env-file.
- Container cred file: `~/.claude/.credentials.json`.
- Onboarding seed: `~/.claude.json`.
- Smoke gate: the bounded `claude -p` auth probe before launch.

## Config shape

claude writes no extra model config file. The container relies on the seeded
credential plus the onboarding state file.

## Install stance

claude is image-baked. No self-install step.

## Launch dialect

- Host preflight: `claude -p <prompt>`.
- Headless: `claude -p --verbose --output-format stream-json <seed>`.
- Interactive: `claude <seed>` plus the seedless TUI flow.

## Smoke gate

The auth probe runs as the agent user, times out if it hangs, and aborts the run
on empty output or timeout. It also checks for the Docker-disk stall case before
blaming auth, so a full disk does not look like a bad login. `WARD_SMOKE_TEST_SKIP=1`
bypasses it.

## See also

- [docs/agent-credentials.md](agent-credentials.md) - the shared cloud credential channel.
- [docs/agent.md](agent.md) - the roster and roles vs harnesses split.
