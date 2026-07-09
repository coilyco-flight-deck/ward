---
doc_goal: Make a self-hoster understand the compiled-in owner allowlist as the deliberate wall around the container's bypassPermissions autonomy, why every dispatch surface enforces it, why it is compiled in rather than runtime config today, and the fork-and-rebuild path that is the only extension until runtime owner config lands.
---
# ward agent: the owner trust gate ([ward#484](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/484))

Point `warded` at an issue in an owner ward does not trust and every dispatch
surface refuses before it does anything:

```
warded: refusing untrusted owner "your-org" (allowed: coilysiren, coilyco-bridge, coilyco-flight-deck, coilyco-gaming). This build dispatches only for its compiled-in primary orgs - see docs/agent-trust-gate.md
```

This page is where that refusal points. It defines the gate, why it exists, and
the current extension story.

## What the gate is

An **allowlist of repository owners** a dispatch will act on. Before ward fetches
the issue or spins a container, it checks the ref's owner (`owner/repo#N`) against
its trusted set. An owner outside the set is refused, fast, with the message above.

The set is ward's **primary orgs**: `coilysiren`, `coilyco-bridge`,
`coilyco-flight-deck`, `coilyco-gaming` (`defaultPrimaryOrgs` in
[`cmd/ward/runner.go`](../cmd/ward/runner.go)). The yes/no check and the
refusal label come from cli-guard's `pkg/ownertrust`, which ward feeds that set
into (`ownerAllowed`, `cmd/ward/agent.go`).

## Why it exists

The in-container agent runs under **`bypassPermissions`** - it acts with no
approval prompts inside its fresh clone (see
[container-permissions.md](container-permissions.md)). An agent that fanned out
into an untrusted owner's repos under that posture would be committing and pushing
to a stranger's repo with the operator's bot identity and no human in the loop.
The gate is the wall: elevated autonomy is granted **only** for owners this build
was compiled to trust.

Every dispatch surface enforces the same check, not just the engineer:

- **engineer** - `resolveAgentWork` gates the ref before any container spins.
- **advisor** - gates the ref it will comment on, and the
  [fan-out](agent-advisor-fanout.md) drops any child spec naming an untrusted
  owner rather than filing it.
- **director** - gates every candidate owner it would dispatch into.

## The extension story

**The set is compiled in and not runtime-extensible today.** There is no env var,
no `.ward/ward.yaml` key, and no `ward` flag that adds an owner to the trusted
set. `primaryOrgs()` returns the hard-coded `defaultPrimaryOrgs()` list and
nothing overrides it.

So a self-hoster on a different org has one path today: **fork ward, add your
owner to `defaultPrimaryOrgs()`, and rebuild.** That is a known sharp edge, not
the intended long-term story:

- [ward#441](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/441) tracks **disclosing** the compiled-in gate before install, so the
  limitation is visible up front rather than only at first refusal.
- [ward#395](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/395) tracks making the trusted set **configurable** rather than compiled in.

Until one of those lands, an untrusted owner is a hard stop with no supported
runtime workaround.

## Why the retired onboarding diagnostics did not catch this

The retired `ward setup` / `ward doctor` surface validated the allowlist and
`security:` probes. It did **not** check "can this host dispatch an agent for
owner X", so on a host where every `warded` call would be refused, the
diagnostic still passed. Closing that day-2 gap is tracked in
[ward#195](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/195).

## See also

- [docs/agent.md](agent.md) - the `ward agent` verb family and usage.
- [docs/container-permissions.md](container-permissions.md) - the `bypassPermissions` posture the gate guards.
- [docs/agent-advisor-fanout.md](agent-advisor-fanout.md) - the per-repo trust gate on cross-repo fan-out.
