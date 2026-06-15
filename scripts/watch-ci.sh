#!/usr/bin/env bash
#
# watch-ci.sh - watch a Forgejo Actions run until every job reaches a terminal
# state, then surface failures with a tail of each failing job's decoded log.
#
# This is an interim BRIDGE script. Its end-state home is a native `ward` verb:
# coily is being retired and its operator surface folds into ward. The
# poll-until-terminal loop below now has a native home - the ward-kdl
# `ops forgejo action ci-watch` complex action (cli-guard#140) - but three
# things keep this a script for now, not yet `ward ci watch`:
#   1. That action lives in the generated `ward-kdl` binary, and the
#      `ward-kdl ops forgejo` surface is not reachable from where this runs yet
#      (only `ward`/`coily` are installed, and `ward` has no `ops`).
#   2. Latest-run defaulting isn't in cli-guard v1 (the action needs --run), so
#      this script still resolves the latest run as a pre-flight.
#   3. The forgejo task-logs primitive it needs lives only in coily today; ward
#      has `list tasks` (via ward-kdl) but no logs surface yet (gitea#35176).
# Tracked as the migration follow-up in docs/ci-watch.md.
#
# Backend: the audited forgejo task surface. Today that is coily's
# `ops forgejo actions task {list,logs}`; as the surface lands in ward, point
# WARD_CI_BRIDGE at the ward equivalent - one env var, no rewrite.
#
# Usage:
#   scripts/watch-ci.sh [REPO] [RUN]
#     REPO  owner/name              (default: coilyco-flight-deck/ward)
#     RUN   run number, e.g. 121    (default: latest run in the listing)
#
# Env:
#   WARD_CI_BRIDGE   task-surface command   (default: "coily ops forgejo actions task")
#   WATCH_CI_INTERVAL  poll seconds         (default: 10)
#   WATCH_CI_TIMEOUT   max wait seconds     (default: 1800)
#   WATCH_CI_LIMIT     task-list page size  (default: 40)
#   WATCH_CI_TAIL      log tail lines       (default: 40)
#
# Exit: 0 all jobs passed; 1 one or more jobs failed; 2 timed out; 3 no run found.
set -euo pipefail

REPO=${1:-coilyco-flight-deck/ward}
RUN=${2:-}
BRIDGE=${WARD_CI_BRIDGE:-coily ops forgejo actions task}
INTERVAL=${WATCH_CI_INTERVAL:-10}
TIMEOUT=${WATCH_CI_TIMEOUT:-1800}
LIMIT=${WATCH_CI_LIMIT:-40}
TAIL=${WATCH_CI_TAIL:-40}

# task-list rows are tab/space-columned: id  status  run#NNN  sha  job  title...
# $BRIDGE is intentionally unquoted so its words split into argv.
list_rows() { $BRIDGE list --repo "$REPO" --limit "$LIMIT"; }
job_logs()  { $BRIDGE logs --repo "$REPO" --id "$1"; }

# Terminal = the run has stopped moving. Anything else (running, waiting,
# queued, blocked, ...) means keep polling.
is_terminal() {
  case "$1" in
    success | failure | failed | cancelled | canceled | skipped | error) return 0 ;;
    *) return 1 ;;
  esac
}

rows_for_run() { awk -v r="run#$RUN" '$3 == r'; }

elapsed=0
while :; do
  rows=$(list_rows)

  # Default to the highest run number present in the listing.
  if [ -z "$RUN" ]; then
    RUN=$(printf '%s\n' "$rows" | awk '{print $3}' | sed 's/run#//' |
      grep -E '^[0-9]+$' | sort -n | tail -1)
    if [ -z "$RUN" ]; then
      echo "no runs found for $REPO" >&2
      exit 3
    fi
  fi

  run_rows=$(printf '%s\n' "$rows" | rows_for_run)
  if [ -z "$run_rows" ]; then
    echo "run#$RUN not found in the last $LIMIT tasks for $REPO" >&2
    exit 3
  fi

  pending=0
  while read -r _id status _rest; do
    is_terminal "$status" || pending=$((pending + 1))
  done <<<"$run_rows"

  if [ "$pending" -eq 0 ]; then
    break
  fi

  if [ "$elapsed" -ge "$TIMEOUT" ]; then
    echo "timed out after ${TIMEOUT}s with $pending job(s) still running on run#$RUN" >&2
    exit 2
  fi
  printf 'run#%s: %s job(s) still running, polling in %ss...\n' "$RUN" "$pending" "$INTERVAL" >&2
  sleep "$INTERVAL"
  elapsed=$((elapsed + INTERVAL))
done

echo "run#$RUN on $REPO:"
printf '%s\n' "$run_rows" | awk '{printf "  %-10s %s\n", $2, $5}'

fails=$(printf '%s\n' "$run_rows" | awk '$2=="failure"||$2=="failed"||$2=="error"{print $1"\t"$5}')
if [ -z "$fails" ]; then
  echo "all jobs passed."
  exit 0
fi

echo
echo "failing job logs (last ${TAIL} lines each):"
while IFS=$'\t' read -r id job; do
  echo "---- ${job} (task ${id}) -------------------------------------------"
  job_logs "$id" 2>&1 | tail -n "$TAIL"
done <<<"$fails"
exit 1
