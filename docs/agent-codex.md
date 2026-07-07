---
doc_goal: Convey the codex harness as ward's open-sandbox cloud driver and give an operator its concrete config posture, install stance, and launch dialect so a codex-driven run is predictable.
---
# ward agent codex

`codex` is the cloud harness with the open sandbox posture.

## Capabilities

- Host credential channel: `WARD_CODEX_AUTH_B64` in the private env-file.
- Container cred file: `~/.codex/auth.json`.
- Config composer: writes `~/.codex/config.toml`.

## Config shape

The bootstrap writes:

- `approval_policy = "never"`
- `sandbox_mode = "danger-full-access"`
- default model / reasoning / verbosity knobs, overridable by `WARD_CODEX_*`

Read `danger-full-access` and `never` as **deliberate containment, not a lax
default**. codex's own sandbox and approval prompts would be redundant here
because the **ephemeral least-access container is already the isolation
boundary** - the run is fenced by repo-scoped credentials, cli-guard policy, and
the audit trail regardless of what codex does inside it. Turning codex's inner
sandbox off lets it work unprompted while ward's outer boundary, not codex's,
holds the reach line.

## Install stance

codex ships in the dev-base image and launches today - no self-install step. The
launcher's one drop-to-shell fallback fires only when an agent binary is **absent**
from the image (in practice just `opencode`, if its self-install fails). codex
is baked in, so `--driver codex` launches the harness rather than dropping to a
shell.

## Launch dialect

- Host preflight: none.
- Headless: `codex exec <seed>`.
- Interactive: `codex <seed>`.

## Startup trust

codex now inherits the same workspace trust set as claude at bootstrap, seeded
into `~/.codex.json` alongside its `~/.codex/AGENTS.md` context load point. A
fresh codex launch in a new workspace starts with the target clone, `/workspace`,
and any granted or warmed sibling repos already trusted.

## Smoke gate

None today. codex dispatch proceeds without the host GO/NO-GO preflight that
[claude](agent-claude.md) runs. The operational consequence: a codex auth failure
has **no preflight backstop**. Where a bad claude credential is caught by the
`claude -p` probe before launch, a codex run seeded with a dead
`~/.codex/auth.json` launches anyway and only fails once inside the container, so
an operator debugging a stalled codex run should suspect credentials directly
rather than expecting a launch-time GO/NO-GO to have flagged them.

## See also

- [docs/agent-credentials.md](agent-credentials.md) - the shared cloud credential channel.
- [docs/agent.md](agent.md) - the roster and roles vs harnesses split.
