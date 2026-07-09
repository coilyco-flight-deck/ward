---
doc_goal: Let an operator pick the right per-run landing policy - direct-main, pull-requests, pull-requests-and-merge, or patch-only - by trust level and understand exactly what each mode changes in the seed, container env/label, and reaper gate, including the honest first-slice limits like the still-push-capable token.
---
# ward agent: workflow modes

`--workflow` picks a dispatched engineer's **landing policy** - how the run is
allowed to turn finished work into a landed change ([ward#508](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/508)). One run, one
container, one issue; the workflow decides only *how it lands*, not *what it
builds*.

```bash
warded engineer #98                          # default: direct-main
warded engineer #98 --workflow direct-main   # merge to main, push, close
warded engineer #98 --workflow pull-requests # branch + pull request, stop there
warded engineer #98 --workflow pull-requests-and-merge # branch + PR + review gate + merge
warded engineer #98 --workflow patch-only    # produce a patch, land nothing
```

The built-in default is `direct-main`. A selected `smart-defaults` bundle can set
a fleet default and per-repo overrides under `agent-workflow`. CLI `--workflow`
wins over both, and a repo override wins over the fleet default. The resolved
mode is visible in `--print` output, rides the container as the `WARD_WORKFLOW`
env var and a `ward.workflow` label, and is read by the reaper.
The old `pr` spelling still parses as a compatibility alias for
`pull-requests`, but the spelled-out names are the primary vocabulary.

## The four modes

- **`direct-main`** - the fast path: implement, commit, merge to
  `main`, push, and close the issue with a `closes #N` trailer. Intended for solo
  repos, trusted automation, and low-review surfaces. On **GitHub** (whose `main`
  is typically protected) `direct-main` already lands via a pull request - the
  forge decides, so the two collapse there.
- **`pull-requests`** - carry the work to a **branch and a pull request** instead
  of landing on `main` directly. The PR is the done-condition; a human or a
  follow-up loop lands it. The seed tells the agent to push the branch and open
  the PR, and **not** to merge it.
- **`pull-requests-and-merge`** - carry the work to a branch and a pull request,
  wait for green checks, run the review gate, then merge with a merge commit.
  This is the autonomous PR lane.
- **`patch-only`** - the run has **no landing authority**: it commits locally but
  produces a **patch** (`git format-patch origin/main --stdout`) and posts it in a
  comment for a human to review and apply. It neither pushes `main` nor opens a
  PR, and writes no `closes #N` trailer. Intended for untrusted targets,
  experiments, and high-risk work.

## Which mode for which trust level

- **High trust with no review need** (your own solo repo, an automation repo you own) - `direct-main`.
- **Shared / human-gated** (a team repo where changes get looked at before merge) - `pull-requests`.
- **Shared / autonomous PR lane** (the review gate should clear before merge) - `pull-requests-and-merge`.
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
  `patch-only` run is never told to merge to `main`, a `pull-requests` run stops
  at PR open, and a `pull-requests-and-merge` run is told to wait for checks and
  merge with a merge commit. See [agent.md](agent.md) for the seed shape.
- **Container env + label.** Any resolved non-`direct-main` run exports
  `WARD_WORKFLOW=<mode>` and wears a `ward.workflow=<mode>` label; `direct-main`
  omits both.
- **The reaper.** `ward container reap` normally force-lands residual work on
  `main` at teardown. For a `pull-requests`/`pull-requests-and-merge`/`patch-only`
  run it reads `WARD_WORKFLOW` and **refuses to force-push `main`**, preserving any
  residual work on a `ward-salvage/<id>` branch instead. See
  [container-reap.md](container-reap.md).

## Rough edges (first slice)

This first slice is deliberately minimal:

- `patch-only` is enforced at the **prompt + reaper** layer, not the credential
  layer: the container still carries a push-capable token. Hard credential scoping
  (a read-only token) is the deferred "read-only credential hardening".
- A `pull-requests`/`pull-requests-and-merge`/`patch-only` run's residual local commits are preserved on a salvage
  branch by the reaper. When Forgejo PRs are available, the reaper also opens a
  PR for the salvage branch and links it from the salvage comment.
- The autonomous [pre-flight](agent-preflight.md) still reads in merge-to-main
  terms; it judges feasibility, not the landing contract, so it is left as-is.

## See also

- [agent.md](agent.md) - the `ward agent` verb family and the seed prompt.
- [container-reap.md](container-reap.md) - the teardown reaper the workflow gates.
- [agent-github.md](agent-github.md) - the GitHub lane, where `direct-main` already lands via a PR.
