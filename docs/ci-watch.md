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
own. It is a script today for two concrete reasons:

- **ward-kdl can't host it.** ward-kdl is spec-driven: the specverb engine maps
  one swagger operation to one leaf, deny-by-default, with no hand-written Go
  (see [ops-forgejo.md](ops-forgejo.md)). A poll-until-terminal loop that then
  fetches logs is composite control flow, not a single REST call. The native
  verb is therefore hand-written gated Go in `cmd/ward`, the same shape as
  `git` / `pkg` / `upgrade` - not a ward-kdl leaf.
- **The logs primitive isn't in ward yet.** ward-kdl already grants
  `list tasks` (the `ListActionTasks` leaf), but there is no task-logs leaf, and
  the Forgejo logs endpoint is a blob redirect rather than a clean scalar op.
  Until ward owns a logs surface, the script reads logs through coily's
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

1. cli-guard: add a task-logs surface (specverb leaf if the endpoint can be
   expressed, else a hand-written gated path).
2. ward `cmd/ward`: a `ci watch` verb wrapping `list tasks` + the logs surface
   with the poll loop, audited and cli-guard-gated.
3. Retire `scripts/watch-ci.sh` and this doc's "why a script" section.
