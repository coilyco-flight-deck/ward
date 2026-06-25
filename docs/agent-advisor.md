# ward agent advisor

`ward agent advisor` (public face `warded advisor`) is the **counsel** role of the
startup roster (ward#347): it answers and **writes no code**. It merges the retired
`reply` + `ask` verbs, and **the argument type selects the mode**. See
[docs/agent.md](agent.md) for the roster.

## Usage

```bash
warded advisor coilyco-flight-deck/ward#98 "what would it take to support X?"   # ref (was reply)
warded advisor #98 "deep dive on the root cause" --thoroughness deep
warded advisor "how does the reaper back-stop residual work?"                   # freeform (was ask)
warded advisor "what would break if WARD_GITCACHE moved?" --repo coilyco-flight-deck/ward
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

## Freeform mode: inline answer (was `ask`)

Runs *inside* a fresh container (not a bare host one-shot) so the answer can lean on the
repo clone and operating context. It joins + validates the question, resolves the
context repo (`--repo`, else the cwd's git origin), trust-gates the owner, spins a fresh
attached container, runs the agent one-shot, and streams the answer inline; the
[reaper](container-reap.md) sweeps it on exit. The prompt is explicit that it is
**read-only** (read, search, run read-only commands - never commit, push, or open
anything). The container exports `WARD_ASK=1`.

## `--print`

A ref renders the ref, depth, prompt, and research prompt (it still fetches the issue,
so it needs the Forgejo token); a freeform question renders the question, seed, and
docker plan. Either way it researches/runs nothing and posts nothing.

## See also

- [docs/agent.md](agent.md) - the roster and the `warded` face.
- [docs/agent-engineer.md](agent-engineer.md) - the implement-a-ticket role.
- [docs/agent-attribution.md](agent-attribution.md) - how the comment is signed.
