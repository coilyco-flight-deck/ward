---
doc_goal: Let an operator pick the right per-run landing policy - direct-main, pull-requests, pull-requests-and-merge, or patch-only - by trust level and understand exactly what each mode changes in the seed, container env/label, director merge boundary, and reaper gate, including the honest first-slice limits like the still-push-capable token.
---
# ward agent: workflow modes

`--workflow` picks a dispatched engineer's **landing policy** - how the run is
allowed to turn finished work into a landed change ([ward#508](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/508)). One run, one
container, one issue; the workflow decides only *how it lands*, not *what it
builds*.

```bash
warded engineer #98                          # default: merge to main, push, close
warded engineer #98 --workflow direct-main              # merge to main, push, close
warded engineer #98 --workflow pull-requests            # branch + pull request
warded engineer #98 --workflow pull-requests-and-merge  # branch + PR, director may merge later
warded engineer #98 --workflow patch-only               # push a branch, land nothing else
```

The built-in default is `direct-main`. A selected `smart-defaults` bundle can set a fleet
default and per-repo overrides under `agent-workflow`. CLI `--workflow` wins over
both, and a repo override wins over the fleet default. The resolved mode is
visible in `--print` output, rides the container as the `WARD_WORKFLOW` env var
and a `ward.workflow` label, and is read by the reaper.

## The four modes

- **`direct-main`** - the fast path: implement, commit, merge to `main`, push,
  and close the issue with a `closes #N` trailer. Intended for solo repos,
  trusted automation, and low-review surfaces.
- **`pull-requests`** - carry the work to a **branch and a pull request**
  instead of landing on `main` directly. The PR is the merge gate; a human or a
  follow-up loop lands it, and the worker keeps watching the PR checks after
  opening it until they are green or the failure is genuinely blocked. If a PR
  already exists when the workflow fails, ward comments the same actionable
  failure summary on both the linked issue and the PR, while keeping the issue
  wording unchanged and trimming reservation-lock wording from the PR copy. The
  seed tells the agent to push the branch and open the PR, and **not** to push
  `main`.
- **`pull-requests-and-merge`** - branch + PR like `pull-requests`, but the run is
  not done until the PR is actually merged. This is the narrow director-merge
  lane.
- **`patch-only`** - the run has **no PR or merge authority**: it pushes a
  remote branch and stops. It neither opens a PR nor merges, and writes no
  `closes #N` trailer. Intended for untrusted targets, experiments, and
  high-risk work.

## Which mode for which trust level

- **High trust with no review need** (your own solo repo, an automation repo you own) - `direct-main`.
- **Shared / reviewed** (a team repo where changes get looked at) - `pull-requests`.
- **Director-merge lane** (ward-owned PRs that meet the review gate and end `WARD-OUTCOME: done`) - `pull-requests-and-merge`.
- **Low trust / exploratory** (an external target, a risky change you want to eyeball
  before it touches the tree) - `patch-only`.

## Smart-defaults shape

```kdl
smart-defaults {
    agent-workflow default="direct-main" {
        repo "coilyco-flight-deck/ward" workflow="pull-requests-and-merge"
    }
}
```

Invalid configured workflow values fail closed during dispatch, before a
container launches.

## What the mode actually changes

- **Seed prompt / done-condition.** The carry clause and the closing
  retrospective's "only after ..." landing phrase both shift with the mode, so a
  `patch-only` run is never told to merge to `main`, a `pull-requests`
  run is never told to treat PR creation as the merge boundary, and a
  `pull-requests-and-merge` run records that the merge must happen before success.
  See [agent.md](agent.md) for the seed shape.
- **Container env + label.** Any resolved non-`direct-main` run exports
  `WARD_WORKFLOW=<mode>` and wears a `ward.workflow=<mode>` label; `direct-main`
  omits both.
- **The cleanup path.** `ward container reap` normally force-lands residual work
  on `main` at teardown. For a `pull-requests`/`pull-requests-and-merge`/`patch-only`
  run it reads `WARD_WORKFLOW` and **refuses to push `main`**, preserving any
  residual work on a `ward-salvage/<id>` branch instead. A clean non-
  `direct-main` run is treated as done at its own boundary. See
  [container-reap.md](container-reap.md).

## Rough edges (first slice)

This first slice is deliberately minimal:

- `patch-only` is enforced at the **prompt + reaper** layer, not the credential
  layer: the container still carries a push-capable token. Hard credential scoping
  (a read-only token) is the deferred "read-only credential hardening".
- A `pull-requests`/`patch-only` run's residual local commits are preserved
  on a salvage branch by the reaper. When Forgejo PRs are available, the reaper
  also opens a PR for the salvage branch and links it from the salvage comment.
- `pull-requests` runs keep watching PR CI/checks after the PR opens, so
  `WARD-OUTCOME: done` only follows green checks or a genuine block. `pull-requests-and-merge`
  waits for the merge itself before it reports success.
- The autonomous [pre-flight](agent-preflight.md) still reads in merge-to-main
  terms; it judges feasibility, not the landing contract, so it is left as-is.

## See also

- [agent.md](agent.md) - the `ward agent` verb family and the seed prompt.
- [container-reap.md](container-reap.md) - the teardown reaper the workflow gates.
- [agent-github.md](agent-github.md) - the GitHub lane, where the target forge decides its own landing policy.
