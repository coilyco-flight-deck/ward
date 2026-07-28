---
doc_goal: Record the bounded diagnosis for the release run that exposed the stable asset verification tag handoff.
---
# release run 2501

This checkpoint records the failed `release.yml` run after
[ward#1465](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1465)
landed on `main`.

## Run

- run:
  [#2501](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/2501)
- internal run id: `12488`
- workflow: `release.yml`
- ref: `release`
- sha: `3e147871ad696ae50c518767b998762aa641ae82`
- title: `merge: remote main before issue-1465 landing`

## First actionable failure

The `release` job passed: checkout, tag selection, `ward:release` image alias
verification, release notes, and draft Forgejo release creation all succeeded.
The dependent `promote-draft-assets` job also checked out the repo, fetched
the staged draft assets, and uploaded the full stable asset set to Forgejo.

The first actionable failure was in `promote-draft-assets`, step
`Verify stable release assets before publication`:

```text
scripts/verify-release-assets.sh: line 5: RELEASE_TAG: missing RELEASE_TAG
```

The failure happened before the `Publish the Forgejo release after assets land`
step, so the install-channel jobs were skipped.

## Root cause

Run [#2501](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/2501)
does not share the missing-checkout root cause from
[#2497](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/2497),
[#2495](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/2495),
or [ward#1597](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1597).
Those runs failed before `scripts/promote-draft-assets.sh` could run. Run 2501
proved that checkout fix by reaching the draft fetch and stable upload
boundary.

This also does not match the promote refspec failure covered by
[promote-run-2491.md](promote-run-2491.md): promotion had already moved
`release` to the selected SHA and the second-stage release workflow was
running.

The separate root cause was local workflow wiring: `scripts/verify-release-assets.sh`
requires `RELEASE_TAG`, but `release.yml` invoked it without passing
`needs.release.outputs.tag` into the step environment.

## Fix state

Current `main` already contains the fix from
[ward#1604](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1604):
`promote-draft-assets` now passes
`RELEASE_TAG: ${{ needs.release.outputs.tag }}` when invoking
`scripts/verify-release-assets.sh`, and the release contract test pins that
handoff.

Successor run
[#2505](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/2505)
completed successfully on commit `0cb0054c1590a494341faf6886869138f904b6d8`,
which includes the [ward#1604](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1604)
fix. No external runner condition or additional code/config fix is indicated by
the run evidence.
