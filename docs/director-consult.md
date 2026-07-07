---
doc_goal: Explain the director's consult-to-headless conversion interview - why a consult ticket is usually one decision away from headless, and how `warded director consult` walks the consult + untriaged queue with a human to record that decision and flip each ticket up to dispatchable.
---
# ward agent director consult ([ward#493](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/493))

`ward agent director consult` (public face `warded director consult`) runs the
**consult-to-headless conversion interview**: the manual launch-triage loop, encoded.

The 2026-07-01 launch triage proved the mechanic by hand - two sessions converted ~60
consult / unlabeled tickets into headless-dispatchable ones. The expensive part was never the
answer. It was a human **noticing** a ticket was one decision away from dispatchable, framing
the options, and recording the answer where the next agent finds it. The endpoint is
**headless-dispatchable**: the interview drives tickets **up** to headless, not around them.

## The queue

The interview keeps a repo's open issues that are **not already dispatchable**: every
ticket labelled `consult`, plus every **untriaged** one with no automation-mode label yet. A
`headless` / `interactive` ticket never enters. Scope + trust reuse the
[heartbeat](agent-director.md)'s `--repo` / `--org` / config-default path.

## The loop, per ticket

1. **Frame the block.** One batched host one-shot (director's own `--driver`) extracts each
   ticket's **single** blocking item - a DECISION only a human holds (a design fork, the
   intent behind a vague ask), or a FACT a human might misremember - with a 2-4 option set, a
   recommendation, and its consequence.
2. **Interview the human.** The human sees the framed decision + options and picks a
   disposition. **Freeform answers are welcome** (many best answers are off-menu).
3. **Record + flip.** The chosen disposition is written to the forge at once.

## Dispositions

Each maps to a done-condition terminal state, or leaves the ticket:

- **`[h]eadless`** - bake the answer onto the ticket as a **DECISION comment** (framed
  decision + answer + attribution/date), drop `consult`, add `headless`. Now dispatchable.
- **`[k]eep`** - stays `consult` with a **recorded reason** (umbrella, pending access).
- **`[c]lose`** - close as **moot** with a reason.
- **`[m]erge`** - close as **merged into another issue** (`m 512 same root cause`).
- **`[s]kip`** / **`[q]uit`** - defer the ticket / end the run.

An answer can ride inline after the letter (`h go with sqlite`) or be typed at the follow-up
prompt; a blank answer cancels back to the menu, never a silent write.

## What keeps it safe

- **Interactive only.** A human answers each ticket, so no terminal errors out (use
  `--dry-run` / `--print` to preview the queue and write nothing). It dispatches nothing
  itself - the flip to `headless` refills the lane the heartbeat drains.
- **Comment before label.** The DECISION comment posts **before** the label flip, so the
  answer survives a label-write failure; likewise the reason posts before a close.
- **Fail-open framing.** A [barred host read](agent-preflight.md) or an incomplete one means
  no generated material - the interview still runs from the raw bodies, never a fabricated
  decision.

## Why it lives in the director

It answers the drain-surface question ("headless lane drained, what's next?"): the work is
usually upstream, a pile of consult tickets nobody has interrogated into shape. It pairs with
[startup triage](director-startup-triage.md) (which *writes* the `consult` labels this
*converts*) and the automation-mode dispatch ceiling - the flip to `headless` is that gate's
deliberate promotion.

## See also

- [agent-director.md](agent-director.md) - the heartbeat this refills.
- [director-startup-triage.md](director-startup-triage.md) - the autonomous pass that writes the mode labels.
- [agent.md](agent.md) - the `ward agent` roster + `warded` face.
