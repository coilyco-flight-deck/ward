---
doc_goal: Make an operator understand how the review panel's verdicts are persisted and how its calibration numbers (false-negative rate, block precision) are surfaced and computed, so the per-class threshold is tuned against evidence rather than faith - and know exactly which measurement is automatic and which needs a human label.
---
# ward agent review: verdicts and measurement ([ward#134](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/134))

A rubber-stamping panel is worse than none, so the [review panel](dispatch-review.md)
persists every decision and surfaces the calibration numbers its per-class threshold
is tuned against.

## Persisted verdicts

Every panel result is one JSONL row in a sidecar log beside the audit trail, at
`~/.ward/audit/review-panel/<slug>.jsonl`: the class, the gate, the pass tally and
threshold, and each reviewer's verdict, reason, and confidence. The `ward agent review`
verb itself also writes its normal audit row. Span attributes ride the same data once
[#135](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/135) lands.

## The stats reader

`ward agent review stats` aggregates that sidecar log:

```bash
ward agent review stats                       # pass/block/advisory split, per class
ward agent review stats --reverted o/r#12,... # + the false-negative rate
ward agent review stats --json                # machine-readable aggregate
```

- **False-negative rate** - of the diffs the panel passed, the fraction whose issue
  was later reverted or fixed. The dangerous number. Computed by joining the persisted
  passes to the `--reverted` set of issue refs. These calibrate the per-class threshold.
- **Block precision** - of blocks, how many were correct. A blocked diff never merges,
  so its correctness has no automatic ground truth. This needs a human label per blocked
  diff and is left to a follow-up.

## Non-goals

- **Repos the operator does not own.** The trusted-owner gate plus
  `--dangerously-skip-permissions` in-container is safe because the operator owns the
  blast radius. Hardening the container into an audited boundary for stranger repos is a
  separate project.
- **BYO-model onboarding.** The gate assumes the operator's model families are present
  in the container. A public BYO-keys / claude-only-default story is separate.
- **The feed/triage/drain loop.** This is the review gate only.

## See also

- [dispatch-review.md](dispatch-review.md) - the panel gate itself.
- [audit.md](audit.md) - the audit log the panel row sits beside.
