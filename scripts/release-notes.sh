#!/usr/bin/env bash
# Turn a raw commit range into user-facing, categorised release notes.
#
# The upstream tag-bump action emits the release body as a flat `git log` dump
# of conventional-commit subjects (ward#486): internal refactor jargon, bare
# SHAs, and bare `ward#NNN` refs, with no signal to a downstream self-hoster
# about whether an upgrade matters to them. This script reshapes that same range
# into a template that leads with a one-line "does this affect you" verdict,
# surfaces breaking/behavior changes first, groups features and fixes, and folds
# routine internal churn (refactors, docs, chores, tests) under a collapsed
# <details> so it stays present but out of the way. See docs/release.md.
#
# Input: one `%h<TAB>%s` line per commit on stdin (short hash, subject). Produce
# it with `git log --pretty=format:'%h%x09%s' <range>`. Subjects are single-line,
# so a line-based read stays portable across gawk/mawk/busawk (no NUL record
# separator). Reading from stdin keeps the categoriser a pure text transform, so
# scripts/release_notes_test.go drives it with fixtures and no repo state.
#
# Output: Markdown release body on stdout.
#
# Flags:
#   --prev <tag>             previous release tag, for the compare footer
#   --new  <tag>             new release tag, for the compare footer
#   --compare-url <base>     repo web base; with --prev/--new, appends a compare link
#   --breaking-hashes "a b"  space-separated short hashes whose commit body carries
#                            a BREAKING note. Subject `!` markers (feat!:) are
#                            detected here; body-footer breaks are found upstream
#                            (git log --grep) and passed in, since only the subject
#                            reaches this script. AGENTS.md: "Minor API breaks ship
#                            in main with a note in the commit body."
set -euo pipefail

prev_tag=""
new_tag=""
compare_url=""
breaking_hashes=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --prev) prev_tag="${2:-}"; shift 2 ;;
    --new) new_tag="${2:-}"; shift 2 ;;
    --compare-url) compare_url="${2:-}"; shift 2 ;;
    --breaking-hashes) breaking_hashes="${2:-}"; shift 2 ;;
    *) echo "release-notes.sh: unknown arg '$1'" >&2; exit 2 ;;
  esac
done

# One awk pass categorises each line and renders the template; buckets are
# newline-joined strings printed by END. See docs/release.md.
awk -v prev_tag="$prev_tag" -v new_tag="$new_tag" \
    -v compare_url="$compare_url" -v breaking_hashes="$breaking_hashes" '
  BEGIN {
    FS = "\t"
    n = split(breaking_hashes, arr, " ")
    for (i = 1; i <= n; i++) if (arr[i] != "") isbreak[arr[i]] = 1
  }

  # Skip blank lines (e.g. an empty range).
  $0 == "" { next }
  {
    hash = $1
    subj = $2

    # Leading conventional-commit type token, if any.
    type = "other"
    if (match(subj, /^[a-zA-Z]+/)) type = tolower(substr(subj, 1, RLENGTH))

    # Breaking: a `!` header (feat!:, fix(x)!:) or a body note flagged upstream.
    breaking = 0
    if (subj ~ /^[a-zA-Z]+(\([^)]*\))?!:/) breaking = 1
    if (hash in isbreak) breaking = 1

    line = "- " subj " (`" hash "`)"

    if (breaking) { breaks = breaks line "\n"; nbreak++ }
    else if (type == "feat") { feats = feats line "\n"; nfeat++ }
    else if (type == "fix") { fixes = fixes line "\n"; nfix++ }
    else if (type == "perf") { perfs = perfs line "\n"; nperf++ }
    else { internal = internal line "\n"; nint++ }
  }

  END {
    # One-line verdict: the header a self-hoster reads first.
    printf "**Does this upgrade affect you?** "
    if (nbreak > 0)
      print "**Yes.** This release has breaking or behavior changes - read the section below before upgrading."
    else if (nfeat > 0 || nfix > 0 || nperf > 0)
      print "**Maybe.** New features and/or fixes below. No breaking changes flagged."
    else
      print "**Probably not.** Internal changes only (refactors, docs, chores) - upgrading is low-impact."
    print ""

    if (nbreak > 0) {
      print "## Breaking / behavior changes"
      print ""
      print "Read these before upgrading."
      print ""
      printf "%s", breaks
      print ""
    }
    if (nfeat > 0) {
      print "## Features"
      print ""
      printf "%s", feats
      print ""
    }
    if (nfix > 0) {
      print "## Fixes"
      print ""
      printf "%s", fixes
      print ""
    }
    if (nperf > 0) {
      print "## Performance"
      print ""
      printf "%s", perfs
      print ""
    }
    if (nint > 0) {
      print "<details>"
      printf "<summary>Internal changes (refactors, docs, chores, tests) - safe to skip (%d)</summary>\n", nint
      print ""
      printf "%s", internal
      print ""
      print "</details>"
      print ""
    }

    # Standing install note: every release attaches Linux-only ward binaries,
    # unexplained next to the notes until now. See docs/release.md, ward#442.
    print "## Install"
    print ""
    print "The attached `ward-linux-{amd64,arm64}` binaries exist for the container agent path - `ward agent` runs ward inside ephemeral Linux containers. Humans install ward on every OS via Homebrew (`brew install coilyco-flight-deck/tap/ward`), which is why there are no darwin assets here."
    print ""

    # Compare footer, so the raw log is one click away for anyone who wants it.
    if (prev_tag != "" && new_tag != "") {
      if (compare_url != "")
        printf "---\nFull changelog: [`%s...%s`](%s/compare/%s...%s)\n", prev_tag, new_tag, compare_url, prev_tag, new_tag
      else
        printf "---\nFull changelog: `%s...%s`\n", prev_tag, new_tag
    }
  }
'
