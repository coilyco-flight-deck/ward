---
doc_goal: Walk a newcomer from zero to a verifiable warded --print dry run while making the agent driver's real nature land - a governed, forge-locked, owner-gated execution layer, not a generic runner - so the trust gate and endpoint lock read as the containment they are, not as friction.
---
# ward first-run guide

Zero to a verifiable first `warded` dry run. Read [README.md](../README.md) first for what ward is.

## Can you get to a first run today?

The `warded` agent driver is not forge-agnostic - it targets `forgejo.coilysiren.me`
and a fixed owner set, both compiled in and not repointable after install.

- **Owner trust gate** - dispatch refuses owners outside `coilysiren`,
  `coilyco-bridge`, `coilyco-flight-deck`, `coilyco-gaming`, **before** it reads
  `--print` ([agent-trust-gate.md](agent-trust-gate.md)).
- **Endpoint lock** - your own Forgejo needs a source build with edited manifests
  ([ward#441](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/441), configurable path [ward#395](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/395)).

Self-host nothing? You **can** render a `--print` plan today against a trusted
public repo (`coilyco-flight-deck/ward#N`, anonymous read, no token) to confirm
your install. You **cannot** yet drive a live run against your own org without
forking. The plain verb gate (`ward exec`/`git`/`audit`) has none of these limits.

## 1. Prerequisites

`--print` needs only the first two - a live run needs all four.

- **macOS or Linux + Homebrew** - the only install path ([README](../README.md#install)).
- **Docker, running** - each run boots a container, the first live run pulls one
  image ([ward#464](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/464), [container.md](container.md)).
- **Harness login on the host, per `--harness`** - ward seeds your host credential,
  holding none itself: run `claude` once (default) or `codex login`
  ([agent-credentials.md](agent-credentials.md)).
- **The push token** - the bot `FORGEJO_TOKEN` resolves on the host into a private
  `--env-file`, never in argv, and `--print` shows only a placeholder.

## 2. Install and verify

```bash
brew install coilyco-flight-deck/tap/ward   # full tap steps in the README
ward version                                # installed release tag
```

- **`warded` is the agent driver only, not a `ward` alias.** `warded version`
  errors - use `ward version`. `warded` understands only roles and refs.
- **The retired `ward setup` / `ward doctor` surface is documented separately** in [release-planning-setup-doctor.md](release-planning-setup-doctor.md).

## 3. First command: a `--print` dry run

```bash
warded engineer coilyco-flight-deck/ward#467 --harness claude --print
```

Files nothing, runs nothing, spins no container. Use a trusted-owner ref - an
untrusted one refuses (that refusal is the blocker above, not a bug).

## 4. Read the plan

It prints the `issue:`/`title:` fetched, the `name:`/`branch:` a run would create,
the **seeded prompt** (issue body inlined verbatim, [agent-engineer.md](agent-engineer.md)),
and the `docker run` line ending in `--env-file <ward-forgejo-token-envfile>`. A
clean plan proves the ref cleared the trust gate, the container identity is right,
and the token stays out of argv. Nothing was written.

## 5. Going live

Drop `--print` once the plan's `issue:`/`repo:`/`title:` are correct, Docker and
your harness login are ready, and the owner is trusted with a token in place:

```bash
warded engineer coilyco-flight-deck/ward#467   # detached, fire-and-forget
```

A live run first runs a GO/NO-GO [pre-flight](agent-preflight.md), posts a
[reservation comment](agent-reservation.md), and always detaches
([agent-flags.md](agent-flags.md)). Logs drain to `~/.ward/agent-logs/<container>/`
([agent-observability.md](agent-observability.md)). A clean run merges to `main`,
else salvages ([container-reap.md](container-reap.md)). Failed or silent?
[troubleshooting.md](troubleshooting.md), indexed by symptom.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
