---
doc_goal: Make an operator understand exactly what the GitHub mirror carries and what it does not, why Forgejo stays the single source of truth, and how each step now fails loud - so the v0.5.8 silent-freeze failure mode reads as a solved recurrence with a known PAT-rotation fix rather than a mystery.
---
# GitHub mirror scope

`.forgejo/workflows/mirror-to-github.yml` keeps the read-only GitHub mirror
(`github.com/coilyco-flight-deck/ward`) in step with canonical Forgejo. It runs
on every push to `main` and carries git **refs plus front-door metadata**:

- Force-pushes `main`.
- Force-pushes every tag.
- Syncs the repo **description and topics** from canonical Forgejo (see below).
- Scrubs stranded legacy GitHub Release objects (see below).

This workflow does **not** create GitHub Release objects - the release pipeline
does: the same binary matrix + `SHA256SUMS` ships to a GitHub release
per tag ([release-binaries.md](release-binaries.md), [release.md](release.md)).

## Front-door hygiene

GitHub is the external front door: it hosts the public issues and PRs an outside
contributor files (canonical work stays on Forgejo, a maintainer carries an
accepted change across - see [CONTRIBUTING.md](../CONTRIBUTING.md)). Two things
make that door look staffed that a plain ref-mirror does not.

### Templates ride the git push

Templates are tracked files, so the `main` force-push carries them for free:

- `.github/ISSUE_TEMPLATE/{bug_report,feature_request}.yml` - GitHub issue
  forms, surfaced by the README's `/issues/new/choose` link.
- `.github/ISSUE_TEMPLATE/config.yml` - disables blank issues, routes security
  to `SECURITY.md` and contribution flow to `CONTRIBUTING.md`.
- `docs/PULL_REQUEST_TEMPLATE.md` - the PR body (GitHub honours `docs/`); the
  doc-layout hook bars a `.github/*.md`, so it lives here.

### Description and topics: the sync step, not the push

Description and topics are API-only metadata, not git objects, so they froze at
whatever seeded the mirror. The `Sync repo description and topics to
GitHub` step reads the **canonical** values from the Forgejo API (anonymous) and
applies them via `GITHUB_MIRROR_PAT` - `PATCH /repos/{owner}/{repo}` and
`PUT .../topics`. Forgejo stays the **single source of truth**: nothing is
hardcoded, so editing on Forgejo and pushing `main` is enough. It is PAT-gated
and parses JSON with `node` (present for the JS actions), since the `docker`
runner has no `jq`.

## Why

A `github-only-dev` cold-read found the mirror frozen at **v0.5.8** (36 tags)
while canonical raced past **v0.24x** - a current README over a Releases page
~200 versions stale, worse than empty because it misstated currency. Three
compounding defects:

- **Tags never pushed.** `actions/checkout` fetched no tag objects, so
  `git push --tags` pushed nothing. `fetch-tags: true` on the checkout
  backfills every tag and keeps it current.
- **Stranded v0.5.x releases.** Release objects left by the retired
  `.github/workflows` semantic-release run (authored by `github-actions[bot]`)
  sat stale on top. An **author-guarded** scrub step deletes only bot-authored
  releases via the API, so [ward#454](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/454)'s PAT-authored release story survives. It
  is idempotent - once they are gone it no-ops.
- **Silent freeze (the recurrence).** The whole workflow is
  `GITHUB_MIRROR_PAT`-gated; the first cut skipped with `exit 0` on a missing
  PAT, so an expired PAT froze the *entire* mirror behind a green check - the
  [ward#237](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/237) tap failure mode. Each step now **fails loud** (`::error` +
  `exit 1`), so a down PAT shows a red run within minutes. The fix is
  operational: rotate `GITHUB_MIRROR_PAT` in ward -> Settings -> Actions ->
  Secrets, then any push to `main` re-converges. (The release pipeline's own
  GitHub publish step stays a soft skip - it must never fail a Forgejo release;
  see [release.md](release.md).)

## See also

- [release.md](release.md) - the Forgejo-canonical release pipeline.
- [.forgejo/workflows/mirror-to-github.yml](../.forgejo/workflows/mirror-to-github.yml) - the workflow itself.
