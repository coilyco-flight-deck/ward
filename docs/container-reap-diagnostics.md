---
doc_goal: Make a reaper's bad outcome self-diagnosing by explaining the `--- reap diagnostics ---` block - the ward version and how it resolved, HEAD-vs-`origin/main` ancestry with an outright FALSE-salvage call, the decision gate and provenance state - and why it prints before the push and stays secret-free so it survives a dead-PAT teardown and folds into a public issue.
---
# reap diagnostics block ([ward#531](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/531))

Whenever [`ward container reap`](container-reap.md) **salvages or fails**, it dumps
one clearly-delimited diagnostics block - `--- reap diagnostics ---` ...
`--- end reap diagnostics ---` - to stderr **and** folds the same facts into the
salvage notification body (the carried-issue comment or the standalone issue).

## Why

Diagnosing the [#504](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/504) false-salvages (a pre-v0.298.0 reaper salvaging already-landed
runs) took an archaeology dig: diffing deleted code against a log line, mapping
commits to release tags, correlating salvage timestamps against container uptimes.
Every bit of that is one line if the reaper prints its own state when it salvages.
The block exists so a bad outcome self-diagnoses.

## What it carries

- **ward version + how it resolved** - the release running the reaper and whether
  it came from a `WARD_VERSION` / `--ward-version` pin or `releases/latest`
  resolved in-container. The single field that would have made the [#504](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/504)
  false-salvage root cause (a stale-pinned reaper) obvious at a glance.
- **HEAD sha, `origin/main` sha, and the ancestry verdict** - `git merge-base
  --is-ancestor HEAD origin/main` in plain words. When HEAD is **already** on
  `origin/main` the block calls the salvage a **FALSE salvage** outright, since
  that is exactly the false-salvage signature.
- **The decision gate that fired** - which branch of the reap logic tripped
  (no-origin/main, missing provenance, missing closing reference, no run-owned
  landed commit, merge conflict, junk scan, rejected push), plus its reason.
- **Provenance + run-owned-landed state** - present / missing-or-unreadable, and
  the `runProvenanceLanded` verdict, not just "provenance missing".
- **Working-tree summary, container uptime / baked-PAT age** - the uptime and
  token-age already stamped on a salvage stay in the block.

## Why this shape

The block prints **before** the branch/issue push. A dead-PAT salvage can fail the
push and file no issue at all, so the container log (`docker logs`) is the only
surface these facts reach, and it must carry them even when the durable issue could
not be written. When the push does land, the block folds into the issue body too so
it survives teardown. The block stays free of raw secrets so it survives the
redacted-drain governance ([#525](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/525)/[#526](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/526)).

## See also

[docs/container-reap.md](container-reap.md) - the reaper itself.
[docs/container-reap-provenance.md](container-reap-provenance.md) - run provenance.
