---
doc_goal: Record the bounded diagnosis for the failed release run after promote run 2496.
---
# release run 2497

This checkpoint records the failed `release.yml` run after
[ward#1596](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1596)
landed on `main`.

## Run

- run:
  [#2497](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/2497)
- workflow: `release.yml`
- ref: `release`
- sha: `662fe4a3954ccc730ae38a58360b57b1d38a7008`
- title: `docs: record promote run 2491 diagnosis`

## First actionable failure

The first `release` job passed: checkout, tag selection, `ward:release` image
alias verification, release notes, and draft Forgejo release creation all
succeeded. The first actionable failure was in the dependent
`promote-draft-assets` job, step `Fetch the draft assets staged on main`:

```text
bash: scripts/promote-draft-assets.sh: No such file or directory
```

## Root cause

Run [#2497](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/2497)
shared the same release-path root cause as
[#2495](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/2495)
and [#2493](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/2493):
`promote-draft-assets` invoked repo-local scripts without first checking out
the repository. The job starts in a fresh container, so `scripts/` is absent
until `actions/checkout` runs.

This is not the same root cause as promote failures
[#2490](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/2490)
or [#2491](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/2491),
which failed in `promote.yml` while pushing detached `HEAD` to `release`
without a fully qualified destination ref. See
[promote-run-2491.md](promote-run-2491.md).

## Fix state

Current `main` already contains the checkout fix from
[ward#1597](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1597):
`promote-draft-assets` now checks out the repository before fetching the draft
assets, and the release contract test pins that dependency.

Successor run [#2501](release-run-2501.md) proved the checkout fix passed the
missing-script boundary by fetching and uploading the staged draft assets. It
then exposed a later, separate workflow environment bug:

```text
scripts/verify-release-assets.sh: line 5: RELEASE_TAG: missing RELEASE_TAG
```

The release workflow now exports the selected release tag as `RELEASE_TAG`
when invoking `scripts/verify-release-assets.sh`.
