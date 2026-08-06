---
doc_goal: Define every shipped harness adapter, host auth source, required model or endpoint input, preflight, and invocation shape.
---
# Agent harnesses

`--harness` and `--agent` select the same typed adapter. The adapter controls
binary installation checks, credentials, context projection, smoke testing,
invocation, and optional display capabilities. It does not alter Ward authority.

## Claude

* Accepts repository instruction input only from `CLAUDE.md` and writes the
  composed instruction to `.claude/CLAUDE.md`.
* Owns `.claude/skills`, `.claude/settings.json`, `.claude/.credentials.json`,
  and `.claude.json`. It is the only adapter with permission and onboarding
  composers.
* Reads the host subscription login from `~/.claude` and projects only the
  required credential material into the private container home.
* Verifies `claude` is on `PATH`, runs its host one-shot preflight, then invokes
  one-shot work with `claude -p`.
* Is currently the only adapter whose manifest enables Ward's injected live
  dispatch-health status line.

## Codex

* Accepts repository instruction input only from `AGENTS.md` and writes the
  composed instruction to `.codex/AGENTS.md`.
* Owns `.agents/skills`, `.codex/config.toml`, and `.codex/auth.json`. It does
  not create or take ownership of Claude settings or state.
* Reads `~/.codex/auth.json`. On macOS, a missing or empty file falls back to
  Codex CLI's `Codex Auth` login item in Keychain.
* Resolves and serializes auth on the host, then writes it only into the private
  container home. It verifies `codex` is on `PATH`.
* Invokes headless work with `codex exec -- <prompt>` so dash-prefixed prompt
  text cannot become a CLI option.

## Goose

* Accepts repository instruction input only from `.goosehints` and writes the
  composed instruction to `.config/goose/.goosehints`.
* Owns `.agents/skills` and `.config/goose/config.yaml`. It receives no Claude
  or Codex credentials, settings, or state.
* Verifies `goose` is on `PATH` and runs a host one-shot endpoint preflight.
* Requires a model through `WARD_GOOSE_MODEL` or
  `--config agent.goose.model=<model>`. Ward supplies no model default.
* Invokes `goose run --no-session -t` and sends the prompt on stdin.

## OpenCode

* Accepts repository instruction input only from `AGENTS.md` and writes the
  composed instruction to `.config/opencode/AGENTS.md`.
* Owns `.agents/skills`, `.config/opencode/opencode.json`, and its installer
  state under `.opencode`. It receives no other harness's files.
* Bootstrap installs `opencode` when absent and fails if it remains unavailable.
* Requires `agent.opencode.model` plus `agent.opencode.endpoint`, with
  `WARD_OPENCODE_MODEL` and `WARD_OLLAMA_URL` as environment spellings.
* Probes the configured OpenAI-compatible `/v1/models` endpoint, invokes
  `opencode run`, and adds Ward request-correlation headers.

## Input ownership

Ward owns adapter mechanics. Deployment or explicit harness inputs own model,
endpoint, reasoning, and display identity. Git independently resolves author
and committer identity from its normal explicit sources. Operator
`default-harness` selects only the default adapter. Roles, harness display
names, and forge credentials never participate in Git commit attribution.

Each adapter declares its accepted instruction source, native instruction and
skill paths, config, credential, permission, onboarding, state, and ownership
surface. Bootstrap validates that declaration before writing. A missing
compatible repository instruction produces compiled Ward doctrine plus a
diagnostic. Ward never falls back to another harness's source.

## See also

* [config-source.md](config-source.md) - precedence.
* [compat-surface.md](compat-surface.md) - provider matrix.
* [container-contract.md](container-contract.md) - credential projection.
