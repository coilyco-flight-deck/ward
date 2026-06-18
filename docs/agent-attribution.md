# Agent attribution on Forgejo write bodies

Per [ward#155](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/155),
every Forgejo write **ward itself emits** that carries a content body is signed
with the driving agent's identity before it is sent, so a human or another agent
reading the issue/comment/commit can tell who wrote it. The reaper's residual
commit carries a matching `Co-Authored-By` trailer.

## Identity

The identity is derived from the agent mode (`cmd/ward/agent_signature.go`):

| mode   | attribution        |
| ------ | ------------------ |
| claude | `Claude (she/her)` |
| codex  | `Codex`            |
| qwen   | `Qwen`             |
| goose  | `Goose`            |

Only claude carries pronouns today; the rest sign by name. An unrecognized mode
resolves whole to the claude identity, mirroring the claude default elsewhere.

## How it is applied

`signBody` appends an attribution footer to a markdown body, and `commitTrailer`
renders the git `Co-Authored-By` line. Signing happens once, at the
`forgejoClient.createIssue` / `commentIssue` choke points, so every ward-emitted
write body is attributed without each call site remembering to. It is idempotent
- a hidden `<!-- ward-agent-signature -->` marker guards against a double sign.

The mode is read from `WARD_AGENT`, then `WARD_MODE` (the in-container case), or
pinned explicitly via `forgejoClient.withMode` by host-side callers - the
reservation comment, the preflight NO-GO comment, and `ward agent task` issue
filing - that already know the mode rather than inheriting it from the env.

## Out of scope: the ward-kdl specverb path

`ward-kdl ops forgejo` (the spec-driven REST path) assembles its bodies in
cli-guard's `specverb` engine, not in this repo, so its `create issue` /
`comment issue` bodies are **not** signed here. Signing them universally "via kdl
spec" is a body-footer directive on the `wrap` that belongs upstream in
cli-guard; see [docs/ops-forgejo.md](ops-forgejo.md).

## See also

- [docs/container-reap.md](container-reap.md) - the reaper, which signs the salvage issue and commit.
- [docs/agent.md](agent.md) - the `ward agent` runs whose writes are attributed.
- [docs/FEATURES.md](FEATURES.md) - ward feature inventory.
