# Watching CI (`scripts/watch-ci.sh`)

`scripts/watch-ci.sh` watches a Forgejo Actions run until every job reaches a
terminal state, then prints a per-job status table and tails the log of each
failing job. It is the tool that surfaced the `bump-formula` release failure
(see [release.md](release.md)).

```
scripts/watch-ci.sh [REPO] [RUN]
  REPO  owner/name           default coilyco-flight-deck/ward
  RUN   run number, e.g 121  default: latest run in the listing
```

Exit codes: `0` all jobs passed, `1` a job failed, `2` timed out, `3` no run
found. Tunables are env vars (`WATCH_CI_INTERVAL`, `WATCH_CI_TIMEOUT`,
`WATCH_CI_LIMIT`, `WATCH_CI_TAIL`); see the script header.

## Why a script, and where it is going

The watcher's end-state home is a native `ward` verb. coily is being retired and
its operator surface is folding into ward, so a CI watcher is squarely ward's to
own.

The poll-until-terminal loop no longer needs hand-written Go: cli-guard's
**complex actions** layer (cli-guard#140, v0.15.0) lets ward-kdl host a bounded
poll over a granted leaf. That action ships today as
`ward-kdl ops forgejo action ci-watch <owner/repo> --run <n>` - it polls the
`list tasks` leaf every 10s up to 30m until every job of the run is terminal,
then exits non-zero if any failed (see
[ward-kdl.forgejo.guardfile.md](ward-kdl.forgejo.guardfile.md) and ward#90). It
is the native replacement for this script's poll loop.

Three things keep the loop in the script for now:

- **The action's surface is not yet reachable here.** The action lives in the
  generated `ward-kdl` binary; only `ward` and `coily` are installed on PATH,
  and `ward` does not expose `ops` yet. Until the `ward-kdl ops forgejo` surface
  is reachable from where the watcher runs, the script keeps its own loop.
- **Latest-run defaulting is deferred.** cli-guard v1 binds `$run` only when
  `--run` is supplied; an omitted run fails the action's condition closed rather
  than resolving "the latest run in the listing". That resolution is a
  pre-flight this script still does, pending cli-guard's reserved
  `input default <jmespath>` slot (ward#90).
- **The logs primitive isn't in ward yet.** ward-kdl grants `list tasks` (the
  `ListActionTasks` leaf) but there is no task-logs leaf - the Forgejo logs
  endpoint is a blob redirect, not a clean scalar op (gitea#35176). Until a logs
  surface lands, the script tails logs through coily's
  `ops forgejo actions task logs`.

## Backend swap

The script talks to the audited forgejo task surface through one indirection:

```
BRIDGE=${WARD_CI_BRIDGE:-coily ops forgejo actions task}
```

It calls `$BRIDGE list` and `$BRIDGE logs`. As coily's forgejo verbs land in
ward, point `WARD_CI_BRIDGE` at the ward equivalent - no other change. When the
native `ward ci watch` verb ships, this script retires.

## Migration checklist

1. ~~cli-guard: a bounded poll-until-terminal primitive~~ - **done**, shipped as
   the `ci-watch` complex action (cli-guard#140 / v0.15.0, consumed in ward#90).
2. Make the `ward-kdl ops forgejo` surface reachable from the watcher's
   environment (fold into the installed `ward`, or install `ward-kdl`), then
   swap this script's poll loop for the `ci-watch` action.
3. cli-guard: `input default <jmespath>` so the action resolves the latest run
   without `--run`, retiring this script's pre-flight resolution (ward#90).
4. cli-guard: add a task-logs surface (specverb leaf if the endpoint can be
   expressed, else a hand-written gated path) - gitea#35176.
5. ward `cmd/ward`: a native `ci watch` verb over the action + logs surface,
   then retire `scripts/watch-ci.sh` and this doc (ward#88).
