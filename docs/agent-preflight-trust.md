---
doc_goal: Convey why host-side pre-flight is a capability boundary, not a prompt - only a trusted cloud harness may run unsandboxed on the host because a local model that ignores read-only can mutate and push real host checkouts - and show the single chokepoint plus fail-safe classifier that enforce it.
---
# pre-flight trust gate: cloud harnesses only ([ward#162](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/162))

The host [pre-flight](agent-preflight.md) runs the agent **on the host** with full
tool access (shell, git, filesystem) in a scratch cwd, asking only for a read-only
GO / NO-GO judgment. **Capability, not the prompt, is the boundary.** A weak or
misbehaving model that ignores "just give a read" executes the whole task on the
host's real local checkouts before the isolated container ever starts - it can
mutate, commit, and push to any checkout the host user can reach, with no human
watching and no container isolation.

That is the incident that filed
[ward#162](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/162): a
`goose` (ollama qwen) pre-flight ran `git pull`, a config rewrite, and `git
add`/`commit`/`push` against real host repos - none of which a fresh-clone-inside
container run could have touched.

## The gate

Only a **trusted cloud harness** may run a host-side one-shot. Every host reader
routes through one chokepoint (`hostOneShotArgv` in `cmd/ward/agents_wire.go`), so
the wall lives in exactly one place:

- **claude** (cloud credential) - keeps the host one-shot.
- **goose, opencode** (self-hosted / local model) - **barred**; they proceed
  straight to the isolated container run, where the fresh clone is the wall.
- **codex** (cloud) - trusted, but has no host one-shot wired anyway.

The classifier (`internal/agents.LocalModel`) reads a harness as local when its
auth kind is `ollama` / `none` / unset, or it pins a local provider endpoint. It
is **fail-safe**: an unrecognized auth reads as local, so a new harness must opt in
with a cloud auth kind rather than inherit host access by omission.

## What it covers

The same gate guards every unsandboxed host-side read, not just the engineer
pre-flight: the director's dispatch decision and startup triage, the route survey,
and advisor research. A barred harness degrades gracefully - the engineer detaches,
the director falls back to its deterministic rank floor and skips triage, and the
route survey / advisor refuse with a pointer to a cloud driver.

## See also

- [agent-preflight.md](agent-preflight.md) - the GO/NO-GO pre-flight itself.
- [agent-drivers.md](agent-drivers.md) - the cloud vs local harness families.
