# GitHub mirror scope

`.forgejo/workflows/mirror-to-github.yml` keeps the read-only GitHub mirror
(`github.com/coilyco-flight-deck/ward`) in step with canonical Forgejo. It runs
on every push to `main` and carries git **refs only**:

- Force-pushes `main`.
- Force-pushes every tag.
- Scrubs stranded legacy GitHub Release objects (see below).

It does **not** create GitHub Release objects. Canonical releases live on
Forgejo; the mirror's `README.md` points a GitHub arrival at the
[canonical releases page](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/releases).

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
If a future story (ward#454) publishes real releases to the mirror via the PAT,
those carry a different author and the scrub leaves them untouched - so the two
can coexist without ward#454 having to unwind this step first.

The `docker` runner has no `jq`, so the step greps release tag names from the
list endpoint, then resolves and deletes each release by id. It is idempotent:
once the legacy releases are gone it finds nothing and no-ops.

## See also

- [release.md](release.md) - the Forgejo-canonical release pipeline.
- [.forgejo/workflows/mirror-to-github.yml](../.forgejo/workflows/mirror-to-github.yml) - the workflow itself.
