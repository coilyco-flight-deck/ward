---
doc_goal: Record the bounded diagnosis for a failed follow-up promote run.
---
# promote run 2491

This checkpoint records the failed `promote.yml` run after
[ward#1594](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1594).

## Run

- run:
  [#2491](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/2491)
- workflow: `promote.yml`
- ref: `main`
- sha: `22f80d6a8ebe93b9cd691ca624f6d26cd4766262`
- title: `Merge remote-tracking branch 'origin/main' into issue-1594`

## First actionable failure

The gate passed through vet, test, lint, draft asset publish, and the
`ward:release` image alias publish. The first actionable failure was in
`Promote main to release (fast-forward only)`:

```text
error: The destination you provided is not a full refname (i.e.,
Neither worked, so we gave up. You must fully qualify the ref.
hint: 'HEAD:refs/heads/release'?
error: failed to push some refs to 'http://forgejo.forgejo.svc.cluster.local/coilyco-flight-deck/ward.git'
```

## Root cause

Run [#2491](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/2491)
shared the same root cause as
[#2490](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/2490)
and [ward#1595](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1595):
the workflow pushed detached `HEAD` to `release` instead of the full branch
ref `refs/heads/release`.

Current `main` already contains the fix from
[ward#1595](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1595),
and successor run
[#2492](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/2492)
proved the corrected promote path.
