---
doc_goal: Convey dispatch-time run provenance as the mechanism that stops the reaper from crediting an already-landed or pre-reservation commit - what the record captures, how the reaper uses baseline-plus-reservation recency to gate a run as done, and why this is the backbone of the reaper's false-salvage defense rather than a minor bookkeeping file.
---
# ward container reap provenance

The reaper's hardest call is telling a run that **actually landed** apart from
one that only looks landed because a matching commit was already sitting on
`origin/main`. Without a dispatch-time anchor, the reaper could credit a run for
history it never wrote - the already-landed-run class of false salvage - and
either bless a failed run as done or, worse, reopen and re-salvage a run that
genuinely finished. The provenance record is the anchor that closes that gap. It
is the backbone of the reaper's false-salvage defense, not a bookkeeping note.

## What the record captures

`ward container reap` writes a small dispatch-time provenance file into the
target worktree at bootstrap (`writeRunProvenance`, `container_bootstrap.go`),
capturing the run's identity and the remote baseline it started from:

- **`run_id`** - the container / run id, tying the record to this one dispatch.
- **`repo`** - the carried repo the run is landing into.
- **`issue`** - the issue number this run was reserved against.
- **`reserved_at`** - the reservation timestamp (RFC3339), the wall-clock floor
  a real landing commit must be newer than.
- **`baseline_main`** - the `origin/main` sha seen at dispatch, the git floor a
  real landing commit must descend from. An **empty repo** has no `origin/main`
  at dispatch, so this is recorded empty (an establish-main dispatch); the
  reaper's [establish-main](container-reap.md) path owns that landing and never
  leans on this baseline ([ward#599](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/599)).

## How the reaper gates a run as done

On teardown the reaper reads the record back and runs `runProvenanceLanded`
(`container_reap.go`). It walks `baseline_main..origin/main` and accepts the run
as landed only when it finds a commit that clears **all three** gates:

- newer than `baseline_main` (it is in the range at all, so it is not
  pre-existing history),
- newer than `reserved_at` (it postdates the reservation window, so it is not a
  commit that predates this run), and
- carrying `closes #<issue>` in its body (it is this run's own work, not an
  unrelated landing).

A commit that fails any gate is not proof, so the run is salvaged rather than
credited. This is the same fact the diagnostics block surfaces as the
`runProvenanceLanded` verdict (see below), so a human reading a preserved
salvage sees exactly why the recency-plus-reservation proof did or did not hold.

## See also

- [docs/container-reap.md](container-reap.md) - teardown flow this gates.
- [docs/container-reap-diagnostics.md](container-reap-diagnostics.md) - the
  `runProvenanceLanded` verdict this record feeds, printed before the push.
- [docs/FEATURES.md](FEATURES.md) - inventory.
