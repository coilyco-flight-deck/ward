---
doc_goal: Explain the in-container code-review gate - where it runs, why the worker's own harness is the default reviewer, the tiers, the thresholds, the skips, and the fail-closed rule.
---
# ward agent: the code-review gate ([ward#134](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/134))

At N concurrent [engineers](agent-engineer.md) the operator is the merge bottleneck.
The panel moves verification off the human step: a worker's diff can survive a
code-review pass in-container, after green CI and before PR open, so only
unsolved diffs reach them. See [dispatch-review-default.md](dispatch-review-default.md)
for the default note.

The gate is `CI green AND quorum >= threshold`, and **fails closed**: a panel error,
timeout, or empty vote blocks landing. The review summary must also show up in the
final `WARD-OUTCOME` comment, not just the panel log.

## Where it runs

The panel is `ward agent review`, wired into the [engineer](agent-engineer.md) seed
when explicitly enabled (not
`remote-branch-only`, which lands nothing). After CI is green and before it opens
the PR or merges, the worker runs it and reads the machine line on stdout -
`WARD-REVIEW: pass` (land), `block` (do not land; post the verdicts and close
`WARD-OUTCOME: blocked`), or `advisory` (only if no reviewer can run at all, and
the host converts that to a fail-closed block). For `pull-request` runs, opening
the pull request is not the finish line. The worker keeps watching the PR checks
and loops on failures until they are green or genuinely blocked.
`ward agent review` stays callable for diagnostics. `--skip-review`
drops the clause from the seed, `--skip-preflight` does the same because the
pre-flight and review share one escape hatch, and `--no-review-gate` /
`--no-preflight` stay accepted as aliases. Config defaults use `agent.review.skip`
([agent-flags.md]).

Running **in-container** keeps the reviewers on the **live worktree**, against the
same filesystem state and CI artifacts the worker produced.

## Worker-first default

Reviewers default to the **worker's own harness family first**. The worker runs the
harness non-iteratively against the live filesystem state with the hand-curated
code-review skill from `agentic-os`. Other families stay available as higher-cost
fallbacks, but the gate no longer degrades to advisory-only just because the worker's
own family is the only one available.

## Cost tiers and refute-by-default

- **The worker's own harness is the free tier** - runs on every diff.
- **Other harnesses are the paid tier** - run as a tiebreaker (free tier blocked) or
  on a high-risk class, never on a clean free-tier pass.

Each reviewer is told to **assume the diff is wrong** and **default to BLOCK on
uncertainty** - a pass with no attempted refutation is not a pass. It gets the issue
contract, the diff, the CI output, and the live tree, and returns
`{"verdict": "pass|block", "reason", "confidence"}`. An unparseable, empty, or errored
reviewer is a **block**.

## Per-class threshold

The quorum tiers by an **autonomy class**, a per-run knob (`--review-class` at
dispatch, riding in as `WARD_REVIEW_CLASS`; `--class` on the command):

- **`lint-cleanup`** - clears on **1** passing reviewer.
- **`default`** - clears on a **majority** of the panel.
- **`refactor`** - needs the panel **unanimous**.

The class is pinned by the host into the container env, never read from the
(untrusted) worker, and is designed to later key off the label
[director triage](director-startup-triage.md) already assigns.

## Single-family fallback

If no reviewer can actually run (binary missing, auth missing, endpoint unreachable),
the host treats the panel as a fail-closed block and writes the review summary into
the conclusion comment. A skipped review is explicit in the conclusion comment too,
so a human can tell a deliberate bypass from an unavailable reviewer.

## See also

- [agent-engineer.md](agent-engineer.md) - the detached worker the gate fronts.
