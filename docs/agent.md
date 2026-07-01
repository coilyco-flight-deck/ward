# ward agent

`ward agent` is **the** entrypoint to the ephemeral [container](container.md)
subsystem: take a Forgejo issue and put an agent on it end to end, sharing the
container bring-up Go directly (it does not shell out). ward#263 retired the old
hand-run `ward container up`/`exec`/`down`/`ls` verbs, so `ward agent` is the single
launch surface; one line replaces a full bring-up stack plus a prompt.

## The `warded` public face

`warded` is the user-facing command: a thin `ward` symlink the multicall rewrite turns
into `ward agent <args>`, not a second code path (ward#247, ward#282). So `warded #98`
*is* `ward agent #98`. Read "warded" as a protective circle - the deny-list and
allowlisted verbs bounding the agent's reach. Everything below applies to both spellings.

## The startup-role roster (ward#347, ward#353)

The surface is a roster of **startup roles** - short nouns that read like a team, not
modes you invoke. You do not run `backlog`, you send in your **director**. The **argument
type** keys the mode (a ref acts on an issue, freeform text files/answers it):

- **`engineer`** (was `headless`+`task`) - implements a ticket end to end, **detached only**
  (ward#356): a ref carries it fire-and-forget, freeform text files one first then carries
  it; interactive work goes to the director. [agent-engineer.md](agent-engineer.md).
- **`director`** (was `backlog`) - autonomous backlog supervisor: dispatches engineers,
  polls outcomes, drains the lane, and **surfaces a read-only scope + dispatch session** on
  drain. [agent-director.md](agent-director.md).
- **`advisor`** (was `reply`+`ask`) - answers, writes no code: a ref comments, freeform
  is interactive (`--oneshot`/no-TTY streams one answer; ward#388).
  [agent-advisor.md](agent-advisor.md).

ward#353 folded the old standalone `architect`/`explore` role into the director's surface
phase, so the roster is now three; the read-only surface survives as [the director's
surface session](agent-surface.md). `warded architect`/`explore`/`sandbox` now error as
unknown commands.

The startup roles are the **roles axis**. Per-agent docs live under
`internal/agents/<name>`.

## Usage

```bash
warded coilyco-flight-deck/ward#98              # bare ref -> engineer run (fire-and-forget)
warded #98                                      # owner/repo inferred from the cwd's git origin
warded engineer #98                             # implement a ticket: detached fire-and-forget
warded engineer "fix the flaky exec_gate test"  # freeform -> file an issue first, then carry
warded director --repo owner/name               # autonomous headless-lane loop; surfaces a read-only session on drain
warded advisor #98 "what would it take to..."   # research the issue, post a comment
warded advisor "how is the audit log written?"  # freeform: interactive (--oneshot = one answer)
```

The role comes first; `--driver` picks the harness (`claude|codex|opencode|goose`, default
claude; ward#185). **A bare ref with no role word runs the `engineer` role** (ward#282,
ward#347). The ref is `owner/repo#N`, a full Forgejo URL, or a bare `#N` / `N` inferring
`owner/repo` from the cwd's git origin; any query string or `#fragment` is ignored.

## Topics

- [agent-roster.md](agent-roster.md) - the generated flat list of every role (one row each; `ward agent roster`).
- [agent-subcommands.md](agent-subcommands.md) - the three roles compared + the reaper.
- [agent-surface.md](agent-surface.md) - the director's read-only surface (was the architect role).
- [agent-preflight.md](agent-preflight.md) - the detached GO/NO-GO pre-flight.
- [agent-wrong-repo.md](agent-wrong-repo.md) - the WRONG-REPO blind-fire path.
- [agent-reservation.md](agent-reservation.md) - reservation, TTL, `--force`, stale warn.
- [agent-flags.md](agent-flags.md) - launch flags and `--details`.
- [container.md](container.md) - the container model (ephemeral, fresh-clone, reaper).
