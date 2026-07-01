# ward agent advisor

`ward agent advisor` (public face `warded advisor`) is the **counsel** role of the
startup roster (ward#347): it answers and **writes no code**. It merges the retired
`reply` + `ask` verbs, and **the argument type selects the mode**. See
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

## Ref mode: research + comment (was `reply`)

A ref plus a prompt: advisor does a one-shot research pass and posts the answer **as a
comment on that issue**. It runs as a **host one-shot** on the same self-assessment slot
the pre-flight and route survey use, so it is only wired for `claude` (`claude -p`) and
`goose` (`goose run -t`); `codex`/`qwen` refuse with a pointer to a supported mode.

It validates the ref + prompt + `--thoroughness`, trust-gates the owner (the comment
posts under ward's bot identity), resolves the issue + thread (ward's own bookkeeping
stripped), researches in a neutral temp dir (never the dispatch cwd), and posts the
agent's stdout wrapped in a ward header + footer, signed via
[agent attribution](agent-attribution.md).

### Thoroughness (`--thoroughness`, alias `--depth`)

Scales the steer and the timeout; an unknown value is a hard error.

| level | timeout | steer |
| --- | --- | --- |
| `quick` | 3m | direct answer from issue + thread; no spelunking |
| `standard` (default) | 8m | reason it through; investigate where it pays off |
| `deep` | 15m | investigate thoroughly - clone, chase edge cases, cite |

The read runs in a clean dir with no checkout, so the prompt supplies the clone URL and
lets it investigate (clone, web) when the depth warrants.

## Freeform mode: seeded session (was `ask`)

Runs *inside* a fresh container (not a bare host one-shot) so the answer can lean on the
repo clone and operating context. It resolves the context repo (`--repo`, else the cwd's
git origin), trust-gates the owner, and spins a fresh attached container seeded with the
question; the [reaper](container-reap.md) sweeps it on exit. Either way the agent stays
**read-only** - never commit, push, open anything, or carry work to merge.

**Interactive by default (ward#388).** With a terminal attached, the freeform advisor
drops you into a **live seeded session** - it answers, then stays for follow-ups, since a
scratch clone plus operating context is the surface you want to keep poking at. This is
the plain `claude <seed>` launch, seeded with `interactivePrompt`.

**One-shot fallback + escape hatch.** With no TTY (piped, CI, the host-broker dispatch a
[director surface](agent-surface.md) uses) interactive can't work, so it falls back to
the **one-shot streamed answer**: `WARD_ASK=1`, the `claude -p` branch, one inline answer
seeded with `askPrompt`. `--oneshot` (alias `--answer`) forces that path even under a
TTY. The switch is `terminalAttached()`, the same signal driving `plan.TTY`.

## `--print`

A ref renders the ref, depth, prompt, and research prompt (it still fetches the issue,
so it needs the Forgejo token); a freeform question renders the question, seed, and
docker plan, stating which path will run (interactive vs one-shot `WARD_ASK=1`). Neither
researches, runs, nor posts anything.

## See also

- [docs/agent.md](agent.md) - the roster and the `warded` face.
- [docs/agent-engineer.md](agent-engineer.md) - the implement-a-ticket role.
- [docs/agent-attribution.md](agent-attribution.md) - how the comment is signed.
