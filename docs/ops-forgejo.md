# ward-kdl (spec-driven verb proving ground)

`ward-kdl` is a **temporary** parallel CLI that proves cli-guard's `specverb`
engine against the real Forgejo API before the spec-driven path folds into
`ward` proper ([ward#62](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/62)).
It is not shipped or installed; build and run it locally.

`cmd/ward-kdl/` is **not** a Go module - it commits only policy and locks.
`specverb-gen` materializes the generated `main.go`, `go.mod`/`go.sum`, and the
binary out-of-band in its cache (the uv-venv analog), so the dev-loop `replace`
and the AWS SDK never enter production ward's module and the parent `go build
./...` sees nothing to build here.

## What mounts

`specverb.Mount` generates the whole tree from the Guardfile plus the pruned
spec - no hand-written commands. The forgejo surface is **40 leaves** across
`org`, `label`, `milestone`, `issue`, `release`, `repo`, `pr`, `task`, and
`issue-label`:

```
ward-kdl ops forgejo org get    <org>   # GET    /orgs/{org}
ward-kdl ops forgejo org delete <org>   # DELETE /orgs/{org}
```

Each leaf carries `--dry-run`, `--query`, and `--output`. `delete` leaves are
flagged destructive; `pr` is read-only and the irreversible `repoDelete` is
intentionally ungranted.

## No hand-written Go

`cmd/ward-kdl/` commits one Guardfile per API (`ward-kdl.<api>.guardfile.kdl`,
today just `ward-kdl.forgejo.guardfile.kdl`) plus the two locks: each API's
pruned `*.swagger.lock.json` and the shared `specverb.lock` (the frozen dep
graph). The generated `main.go`, `go.mod`/`go.sum`, and binary materialize
out-of-band - none are committed; the driver REF pins a cli-guard release. Build:

```
make build-ward-kdl   # specverb-gen lock (fetch + prune), then build -> bin/ward-kdl
```

## The spec lock (uv-style)

A CLI generated from a live API spec should not silently inherit upstream's
pace. Following uv's model, a committed lock decouples the two cadences:

- **`forgejo.swagger.lock.json`** - the committed lock, `go:embed`'d into the
  generated `main.go`. A normal run is **hermetic**: no network for the spec.
- **`make lock`** - the only step that fetches live and rewrites the lock, the
  deliberate "absorb upstream" move.
- **`make skew`** - fetches live and warns (kubectl-style) on drift.

The lock is **pruned to the granted operations + their transitive `$ref`
schemas** (the consumer's own contract, not the full upstream dump), so it stays
small and reviewable. `resolveSpec` prefers `$WARD_KDL_OPS_FORGEJO_SPEC`, then
the embedded lock, then a bootstrap fetch. The runtime cache stays gitignored.

## Merging multiple APIs

`specverb-gen` merges every `ward-kdl.<api>.guardfile.kdl` sharing the `wrap
ward-kdl` name into one binary, each `ops <api>` its own group. Per API: the spec lock and reference doc; shared: the `main.go` and one `specverb.lock`.

## Seams

The generated `mountOps` loops the specverb seams over each merged API: embedded
Guardfile, spec bytes, `verb.Wrap` over `~/.coily/audit/<slug>.jsonl`, and the SSM
`TokenResolver` (`/forgejo/api-token`). The audit writer is built once; mutating verbs use a redirect-refusing client.

## Proving against the coily oracle

`ward-kdl ops forgejo org create --username probe --dry-run --output json` prints
the resolved `POST /api/v1/orgs` request (auth header redacted) - the same call
coily's oracle makes. Drop `--dry-run` for the live call (then `org delete
<org>` to self-clean; needs AWS creds + network). The engine is unit-tested in
cli-guard; ward-kdl carries no Go to test.

## See also

- [FEATURES.md](FEATURES.md) - ward feature inventory.
- cli-guard `specverb` - the engine ([cli-guard#75](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/issues/75)).
