# Watching CI (`ward ci watch`)

`ward ci watch` watches a Forgejo Actions run until every job reaches a terminal
state, then prints a per-job status table and points at each failing job's run
page. It is the tool that surfaced the `bump-formula` release failure (see
[release.md](release.md)).

```
ward ci watch [owner/repo]
  owner/repo   default coilyco-flight-deck/ward
  --run N      run number to watch (default: latest run in the listing)
  --interval   poll seconds       (default 10,  env WATCH_CI_INTERVAL)
  --timeout    max wait seconds   (default 1800, env WATCH_CI_TIMEOUT)
  --limit      task-list page size(default 40,   env WATCH_CI_LIMIT)
```

Exit codes: `0` all jobs passed, `1` a job failed, `2` timed out, `3` no run
found. The verb is audited (one `ci.watch` JSONL row) and argv-validated like
every ward verb; it is read-only, so it carries no clean-tree gate.

## Native verb, not a script

This verb replaces the retired `scripts/watch-ci.sh` bridge. coily is being
wound down and its operator surface folds into ward, so a CI watcher is squarely
ward's to own.

The poll-until-terminal loop is composite control flow - poll a granted leaf,
branch on aggregate state, loop with a timeout - which the deny-by-default
`specverb` engine (one swagger-op per leaf, no hand-written Go) cannot express.
So `ci watch` is a native hand-written, cli-guard-gated Go verb in `cmd/ward`,
shaped like `git` and `upgrade`. It reads the same Forgejo Actions tasks surface
that `ward ops forgejo tasks list` exposes (`GET .../actions/tasks`),
authenticating as the `coilyco-ops` bot through the SSM-resolved token, then runs
the poll loop, status table, and failure report itself.

cli-guard also ships a bounded-poll **complex action**
(`ward-kdl ops forgejo action ci-watch`, cli-guard#140) that settles a run over
the `list tasks` leaf. `ci watch` does the poll natively rather than shelling
that action because it adds the script's two extra behaviours the action does
not: resolving the latest run when `--run` is omitted, and reporting per-failing
job afterward.

## Failing-job logs

The status table names every failing job and links its run page. Inline tailing
of a failing job's decoded log is **not** done yet: the Forgejo logs endpoint is
a blob redirect, not a clean API op (gitea#35176), so there is no audited
task-logs surface to read through. Until one lands in cli-guard, open the linked
run page for the log. Restoring the script's inline tail is the one follow-up
left from this verb's introduction (ward#88).
