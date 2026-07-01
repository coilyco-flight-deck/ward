# director startup triage (ward#397)

The autonomous-drain half of [`ward agent director`](agent-director.md) was starving.
Nothing assigned the `headless`/`interactive`/`consult` **mode** labels between sessions, so
the [init gate](agent-director.md) always opened onto an **empty** headless lane and only the
surface half ever ran. The heartbeat already **read** mode labels to rank lanes; nothing ever
**wrote** them. director now folds a triage pass into **startup, before the init gate**, so
the gate sees a warm lane.

It rides the launch flow, not an out-of-band cron - the operator's requirement was that it
fit the flow, not add a step - and applies the `tooling-issue-prioritization` method from
[agentic-os](https://forgejo.coilysiren.me/coilyco-flight-deck/agentic-os).

## The pass, per repo in scope

1. **P0 content-net (deterministic recall).** The `p0-content-rules` regexes scan each open
   issue's title+body for incident language: credential leak, arbitrary-code-exec / gate
   bypass, data loss, crashloop / pipeline-down, blocks-all-committed-work. A hit nominates a
   P0 **candidate**. The net over-matches on purpose (a topic mention trips it); precision
   comes from the confirm step, not tighter regexes.
2. **Batched judgment one-shot.** One host one-shot per repo - director's **own** `--driver`,
   the same path the per-tick dispatch decision uses, not a new harness - judges every
   untriaged issue at once. Per issue it returns an urgency `SCORE` (0-3), an automation
   `MODE`, a confidence, and for a P0 candidate an active-incident confirm.
3. **Assemble.** Confirmed candidates become **P0** and leave the pool. The scored remainder
   is **percentile-cut** into P1-P4 bands (top 20% P1, next 20% P2, next 20% P3, bottom 40%
   P4); a pool with no urgency signal all lands P3, the default tier. Mode is **fail-closed**:
   only a confident `headless`/`interactive` promotes out of human-gated, everything else
   (low confidence, an unread field, a garbled line) lands `consult`.
4. **Write.** director adds the computed labels via `ward ops forgejo issue-label add`.

## What keeps it cheap and safe

- **Skip the already-triaged.** A fully-labeled issue (both a tier and a mode) is dropped
  before the one-shot even sees it, so a large backlog's steady state costs almost nothing -
  only newly-filed unlabeled issues get judged. This is the staleness rule: presence of a
  label is the freshness signal, no time-based re-triage.
- **Only the missing axis is written.** A partially-labeled issue (a human set the tier but
  not the mode, say) has only its missing axis written, so an existing human label is never
  clobbered and the percentile cut ranks only the untiered pool.
- **Best-effort, fail-closed.** No host one-shot on this host (codex/qwen have none), or a
  read that doesn't complete, writes **nothing** - it never guesses a promotion. A per-issue
  write failure (e.g. an org label not yet defined) is noted and skipped, never fatal. A repo
  whose backlog read fails is skipped with a note.

## Toggling it

On by default. `--no-triage` skips it. `--dry-run` / `--print` skip it like every other
launch step (they launch nothing). The dispatch gate + org-label rollout the mode axis feeds
is tracked in agentic-os#246; until it lands the labels already serve as a selection filter
for what the drain dispatches.

## See also

- [agent-director.md](agent-director.md) - the heartbeat + init gate this warms.
- [agent.md](agent.md) - the `ward agent` roster + `warded` face.
