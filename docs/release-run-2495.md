---
doc_goal: Record the bounded diagnosis for failed release runs after promotion succeeded.
---
# release run 2495

This checkpoint records the failed `release.yml` run after
[ward#1478](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1478)
landed on `main`.

## Run

- run:
  [#2495](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/2495)
- workflow: `release.yml`
- ref: `release`
- sha: `29c19a7e24554c1fa35e9d9423a688aa5bffe6c4`
- title: `Merge remote-tracking branch 'origin/main' into issue-1478`

## First actionable failure

The first `release` job passed: checkout, tag selection, `ward:release` image
alias verification, release notes, and draft Forgejo release creation all
succeeded. The first actionable failure was in the dependent
`promote-draft-assets` job, step `Fetch the draft assets staged on main`:

```text
bash: scripts/promote-draft-assets.sh: No such file or directory
```

## Root cause

Run [#2495](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/2495)
shared the same release-path root cause as
[#2493](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/2493):
`promote-draft-assets` invoked repo-local scripts without first checking out
the repository. The job starts in a fresh container, so `scripts/` is absent
until `actions/checkout` runs.

This is not the same root cause as promote failures
[#2490](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/2490)
or [#2491](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/2491),
which failed in `promote.yml` while pushing detached `HEAD` to `release`
without a fully qualified destination ref. See
[promote-run-2491.md](promote-run-2491.md).

It also does not point at the cloud credential removal from
[ward#1478](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1478):
the failed release job did not reach any AWS, SSM, registry, tap, or Scoop
credential path before the missing checkout stopped it.

## Fix

`promote-draft-assets` now checks out the repository before fetching the draft
assets, and the release contract test pins that dependency so repo-local script
calls cannot regress silently.
