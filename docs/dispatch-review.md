---
doc_goal: Make an operator understand the in-container adversarial-review panel as the pre-PR quorum gate that raises a diff's trust floor before it lands - where it runs, why heterogeneity is the mechanism, the cost tiers, the per-class threshold, and how it fails closed - so unsupervised merge of a low-risk class is defensible.
---
# ward agent: the adversarial-review panel ([ward#134](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/134))

At N concurrent [engineers](agent-engineer.md) the operator is the merge bottleneck,
because the PR is the only review gate. The review panel moves verification off the
human's step: a worker's diff must survive a heterogeneous multi-model panel **in the
container, after green CI and before the PR opens**, so the operator's queue only sees
diffs the panel could not agree on.

The gate is `CI green AND quorum >= threshold`, and **fails closed**: a panel error,
timeout, or empty vote blocks the landing.

## Where it runs

The panel is `ward agent review`, wired into the [engineer](agent-engineer.md) seed
for every headless landing run (not `patch-only`, which lands nothing). After CI is
green and before it opens the PR or merges, the worker runs it and reads the machine
line on stdout - `WARD-REVIEW: pass` (land), `block` (do not land; post the verdicts
and close `WARD-OUTCOME: blocked`), or `advisory` (only one family available, see
fallback). The exit code mirrors the verdict, so a shell `&&` enforces it too, and
`--no-review-gate` at dispatch drops the clause from the seed.

Running **in-container** beats a separate cloud pass: the reviewers see the **live
worktree**, so they run the exact failing test against the same filesystem state the
worker produced, sharing one clone + CI artifacts across all three agents.

## Heterogeneity is the mechanism

Reviewers must be **different model families from the worker** - it never reviews its
own diff, since correlated blind spots mean its reviewer misses exactly its own
mistakes. The panel excludes the worker's family (worker `claude` gets `opencode` +
`codex`; `claude`, the expensive worker, is never a reviewer).

## Cost tiers and refute-by-default

- **`opencode` (local qwen) is the free tier** - runs on every diff.
- **`codex` (subscription) is the paid tier** - runs only as a tiebreaker (free tier
  blocked) or on a high-risk class, never on a clean free-tier pass.

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

If, after excluding the worker's family, **no heterogeneous reviewer is available**
(binary missing, codex unauthenticated, ollama unreachable), the panel degrades to
**advisory-only**: it does not gate the diff and says so loudly in the log and in a
`PR-BODY-NOTE:` the worker copies into the PR body. Advisory never blocks. A reviewer
harness must be provisioned in the container (binary, credential, endpoint) for a
**binding** panel.

## See also

- [dispatch-review-measurement.md](dispatch-review-measurement.md) - verdicts, stats, non-goals.
- [agent-engineer.md](agent-engineer.md) - the detached worker the gate fronts.
