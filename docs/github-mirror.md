# GitHub mirror scope

`.forgejo/workflows/mirror-to-github.yml` keeps the read-only GitHub mirror
(`github.com/coilyco-flight-deck/ward`) in step with canonical Forgejo. It runs
on every push to `main` and carries git **refs plus front-door metadata**:

- Force-pushes `main`.
- Force-pushes every tag.
- Syncs the repo **description and topics** from canonical Forgejo (see below).
- Scrubs stranded legacy GitHub Release objects (see below).

This workflow does **not** create GitHub Release objects - the release pipeline
does (ward#454): the same binary matrix + `SHA256SUMS` ships to a GitHub release
per tag ([release-binaries.md](release-binaries.md), [release.md](release.md)).

## Front-door hygiene (ward#490)

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
whatever seeded the mirror (ward#438). The `Sync repo description and topics to
GitHub` step reads the **canonical** values from the Forgejo API (anonymous) and
applies them via `GITHUB_MIRROR_PAT` - `PATCH /repos/{owner}/{repo}` and
`PUT .../topics`. Forgejo stays the **single source of truth**: nothing is
hardcoded, so editing on Forgejo and pushing `main` is enough. It is PAT-gated
and parses JSON with `node` (present for the JS actions), since the `docker`
runner has no `jq`.

## Why (ward#477)

A `github-only-dev` cold-read found the mirror frozen at **v0.5.8** (36 tags,
"latest release v0.5.8") while canonical had raced past **v0.24x**. `main`
mirrored current, so the mirror showed a v0.24x-era README sitting on a release
page ~200 versions stale - worse than empty, because it affirmatively misstated
currency. Two defects, two fixes.

### Tags froze - `fetch-tags: true`

`actions/checkout` carried no tag objects into the job, so `git push --tags`
had nothing to push and the mirror's tags stayed at whatever the retired
`.github/workflows` release had last left there (v0.5.8). Setting
`fetch-tags: true` on the checkout fetches every tag, so the force-push
backfills the full set and keeps it current thereafter.

### Stranded release objects - the author-guarded scrub

The v0.5.x Release objects were artifacts of the retired GitHub-Actions
semantic-release run, authored by `github-actions[bot]`. Nothing in the Forgejo
pipeline creates or updates them, so they sat stale at the top of the mirror's
Releases page.

The scrub step deletes them via the GitHub API using `GITHUB_MIRROR_PAT`. It is
**author-guarded**: only releases authored by `github-actions[bot]` are removed.
ward#454 now publishes real releases to the mirror via that PAT (from the
release pipeline, not this workflow); they carry the PAT user as author, so the
scrub leaves them untouched - the two coexist without unwinding this step.

It greps release tag names from the list endpoint, then resolves and deletes
each by id. It is idempotent: once the legacy releases are gone it no-ops.

## See also

- [release.md](release.md) - the Forgejo-canonical release pipeline.
- [.forgejo/workflows/mirror-to-github.yml](../.forgejo/workflows/mirror-to-github.yml) - the workflow itself.
