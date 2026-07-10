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
is baked in, so `--harness codex` launches the harness rather than dropping to a
shell.

## Launch dialect

- Host preflight: none.
- Headless: `codex exec <seed>`.
- Interactive: `codex <seed>`.

## Startup trust

codex inherits the same workspace trust set as claude at bootstrap, seeded as
`[projects."<dir>"]` tables with `trust_level = "trusted"` in `~/.codex/config.toml` -
the location the rust codex CLI actually reads folder trust from ([ward#678](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/678); the
former `~/.codex.json` seed carried claude's schema and codex never read it). A
fresh codex launch in a new workspace - the director's read-only surface included -
starts with the target clone, `/workspace`, and any granted or warmed sibling
repos already trusted, no trust dialog on the first prompt.

## Smoke gate

codex still has no host GO/NO-GO preflight, but headless launches now run a
bounded in-container `codex exec` probe before the model starts. The probe uses
the resolved model and the cheapest sandbox/approval posture, so a stale model
string fails as `model-config` instead of silently falling back. The probe feeds
`Reply with exactly ok.` on stdin through `codex exec -` so Codex does not take
the prompt-argument stdin path. Other codex launch failures still surface
through the same probe as `codex-probe`.

## See also

- [docs/agent-credentials.md](agent-credentials.md) - the shared cloud credential channel.
- [docs/agent.md](agent.md) - the roster and roles vs harnesses split.
