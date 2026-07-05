---
doc_goal: Let a reader pick and drive the advisor counsel role with confidence - the write-no-code member of the guarded roster - grasping how the argument type selects ref-research versus freeform-session mode and what each actually does.
---
# ward agent advisor

`ward agent advisor` (public face `warded advisor`) is the **counsel** role of the
startup roster: it answers and **writes no code**. It merges the retired
`reply` + `ask` verbs, and **the argument type selects the mode**. The advisor
role holds the **live-observe guardfile set** (the tailnet + `~/.aws`) via its
`roles` entry in [`ward-kdl.fleet.kdl`](../.ward/ward-kdl/ward-kdl.fleet.kdl)
([ward#578](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/578)), so ref-mode research reaches live backends and on-host state with no
flag, and `--no-tailnet` opts out when a run should stay fully isolated. See
[docs/agent.md](agent.md).

## Usage

```bash
warded advisor coilyco-flight-deck/ward#98 "what would it take?" --thoroughness deep   # ref (was reply)
warded advisor "how does the reaper back-stop residual work?"                          # freeform: interactive seeded session (ward#388)
warded advisor "summarize the audit-log schema" --oneshot --repo coilyco-flight-deck/ward   # freeform: force the one-shot answer
```

## Argument-type dispatch

`parseAgentIssueRef` succeeds → **ref mode**; it errors on non-ref text → **freeform
mode**. Either way advisor changes no code and carries nothing to merge.

## Ref mode: research + comment or cross-repo fan-out (was `reply`)

A ref plus a prompt: advisor runs a one-shot research pass and either posts the answer
**as a comment on that issue** or, when work spans multiple repos, **fans it out into
per-repo issues** plus an index comment ([agent-advisor-fanout.md](agent-advisor-fanout.md)).
The research runs in a **fresh ephemeral container** ([ward#411](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/411)), like the engineer
and freeform modes and no longer a native host one-shot. Because the container is the
sandbox, **any wired harness** runs it, local models included.

Like every warded role, the advisor lane auto-mounts the target's own
`catalog.dependsOn` upstreams as **read-only** reference clones ([ward#573](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/573),
widened from the advisor-only [ward#566](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/566)).
Ward reads that live config at launch time and clones each declared dependency
read-only under `/workspace`, deduped against the target, the explicit `--repo`
grants, and the `/substrate` set. These ride a separate context slice from the
writable grants, so a run is never asked to push a dependency it only read - the
full model lives in [container-multi-repo.md](container-multi-repo.md).

It trust-gates the owner and resolves the issue + thread **on the host**, then spins a
read-only one-shot container (`WARD_ASK` + `WARD_READONLY`) seeded with the research prompt
and **captures its stdout**. ward parses the captured plan **host-side**, keeping the
fan-out deterministic; every post is signed via [attribution](agent-attribution.md).

The capture is a plain foreground `docker run` that streams stdout but attaches **no
stdin** (no `-i`/`-t`) - the one-shot `claude -p` reads none. That is what lets a
[director surface](agent-surface.md) forward the run through the dispatch broker: a
`-i` stdin attach fought the surface's own TTY and rejected the container pre-start with
docker exit 125 ([ward#606](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/606)).

### Thoroughness (`--thoroughness`, alias `--depth`)

Scales the steer and the timeout. An unknown value is a hard error.

- **`quick`** - 3m - direct answer from the issue + thread, no spelunking.
- **`standard`** (default) - 8m - reason it through, investigate where it pays off.
- **`deep`** - 15m - investigate thoroughly (clone, chase edge cases, cite).

The container clones the repo to read (or other repos / the web when depth warrants). The
per-level timeout bounds the dig, plus a bring-up budget.

## Freeform mode: seeded session (was `ask`)

Runs **inside** a fresh container (not a bare host one-shot) so the answer can lean on the
repo clone and operating context. It resolves the context repo (`--repo`, else the cwd's
git origin), trust-gates the owner, and spins an attached container seeded with the
question; the [reaper](container-reap.md) sweeps it on exit. The advisor lane stays
**read-only**, and any auto-granted context repos are cloned read-only too.

**Interactive by default.** With a terminal attached, the freeform advisor
drops you into a **live seeded session** - the plain `claude <seed>` launch, seeded with
`interactivePrompt` - so you can keep poking at the scratch clone after the first answer.

**One-shot fallback + escape hatch.** With no TTY (piped, CI, the host-broker dispatch a
[director surface](agent-surface.md) uses) it falls back to the **one-shot streamed
answer**: `WARD_ASK=1`, the `claude -p` branch, seeded with `askPrompt`. `--oneshot`
(alias `--answer`) forces that path even under a TTY, via `terminalAttached()`.

## `--print`

A ref renders the ref, depth, prompt, and research prompt (it still fetches the issue, so
it needs the Forgejo token); a freeform question renders the question, seed, and docker
plan, stating which path runs. Neither researches, runs, nor posts anything.

## See also

- [docs/agent.md](agent.md) - the roster and the `warded` face.
- [docs/agent-engineer.md](agent-engineer.md) - the implement-a-ticket role.
- [docs/agent-advisor-fanout.md](agent-advisor-fanout.md) - the structured emit and cross-repo fan-out.
- [docs/agent-attribution.md](agent-attribution.md) - how the comment is signed.
