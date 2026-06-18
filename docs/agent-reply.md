# ward agent reply

`ward agent <name> reply <issue-ref> <prompt>` is the one-shot research surface of
[`ward agent`](agent.md): point it at a Forgejo issue and a question, and it does a
single research/investigation pass at a chosen depth and posts the result **as a
comment on that issue**. Unlike `work`/`headless`/`task` it never spins a container,
touches code, or carries the issue to merge - the whole job is one good comment
(ward#179).

## Usage

```bash
ward agent claude reply coilyco-flight-deck/ward#98 "what would it take to support X?"
ward agent claude reply coilyco-flight-deck/ward#98 "deep dive on the root cause" --thoroughness deep
ward agent claude reply coilyco-flight-deck/ward#98 "quick gut check" --print   # show the prompt, post nothing
```

The signature mirrors `task` with the arguments swapped to fit a reply: **arg0 is
the issue ref** (`owner/repo#N` or a full Forgejo URL, query/fragment ignored) and
**the rest is the prompt**. The prompt is canonically one quoted argument, but any
trailing words are joined so an unquoted multi-word prompt still works.

`<name>` is the mode. reply runs as a **host one-shot** on the same self-assessment
slot the [pre-flight](agent.md) and [route survey](agent-task.md) use, so it is only
wired for `claude` (`claude -p`) and `goose` (`goose run -t`); `codex`/`qwen` refuse
with a pointer to a supported mode.

## What it does

1. **Parse + validate** the ref, the prompt (both required), and `--thoroughness`.
2. **Trust-gate** the owner (primary-org set), since the comment posts under ward's
   bot identity - the same gate `work`/`task` apply.
3. **Resolve** the issue (failing fast on a bad ref) and its thread; ward's own
   bookkeeping comments are stripped so only real content shapes the read.
4. **Research** via the mode's agent one-shot in a neutral empty temp dir (never the
   dispatch cwd; mirrors the pre-flight), echoing the read to your terminal.
5. **Post** the agent's stdout as the comment, wrapped in a ward header (question +
   depth) and a footer flagging it as research to verify, signed with the mode's
   [agent attribution](agent-attribution.md). ward prints the issue URL.

The prompt is explicit that the agent is **not** implementing anything and that its
stdout *is* the comment, so it writes the answer in clean markdown with no preamble.

## Thoroughness (`--thoroughness`, alias `--depth`)

The level scales both the prompt steer and the timeout, so a deep dive isn't cut off
mid-investigation. An unrecognized value is a hard error, not a silent downgrade.

| level | timeout | steer |
| --- | --- | --- |
| `quick` | 3m | direct answer from the issue + thread + what it knows; no spelunking |
| `standard` (default) | 8m | reason it through; investigate further only where it pays off |
| `deep` | 15m | investigate thoroughly - clone the repo, chase edge cases, cite findings |

The read runs in a clean scratch dir with no checkout, so the prompt supplies the
repo's clone URL and tells the agent it may investigate further (clone, web) when
the depth warrants - but never to assume a local checkout exists.

## `--print`

Resolves the issue and renders the ref, depth, reply prompt, and the full research
prompt, then exits without researching or posting. Like `work --print` it still
fetches the issue (to fill the rendered prompt), so it needs the Forgejo token.

## See also

- [docs/agent.md](agent.md) - the `ward agent` umbrella.
- [docs/agent-task.md](agent-task.md) - `task`, whose signature reply mirrors.
- [docs/agent-attribution.md](agent-attribution.md) - how the comment is signed.
