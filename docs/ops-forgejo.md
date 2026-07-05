---
doc_goal: Convey ward-kdl as the build-time generator that proves cli-guard's specverb/execverb engines - a guardfile plus a pruned committed spec lock compiled into an audited ops CLI with no hand-written Go - so a reader sees this as the load-bearing source-in-artifact-out layer feeding the shipped binary, not a throwaway scratch tool.
---
# ward-kdl (the ops-verb generator)

ward-kdl is one of ward's three load-bearing layers, told apart by **when** each
runs. [cli-guard](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard) is
the **engine** (deny-by-structure routing, OpenAPI to audited verb). **ward-kdl**
is the **build-time generator**: a KDL guardfile plus a pruned committed spec lock
go in, an audited `ops <api>` CLI comes out, no hand-written Go. **ward** is the
**run-time product** that embeds those generated surfaces. So ward-kdl feeds
production, not a scratch bench: its forgejo output lands in the shipped binary as
`ward ops forgejo`, mounted from the same guardfiles - see
[ops-forgejo-in-ward.md](ops-forgejo-in-ward.md).

The standalone `ward-kdl` binary proves cli-guard's `specverb` and `execverb`
engines - one merged binary holding `ops forgejo` (spec REST) and `ops aws`
(exec) - and is where the generated path is exercised before it folds into `ward`
proper
([ward#62](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/62)).
That binary is not itself shipped or installed, so build and run it locally.
Keeping the binary local does not make the layer temporary: the guardfiles, locks,
and generated verbs it produces are the source of the ops surface every `ward`
carries.

`.ward/ward-kdl/` is **not** a Go module - it commits only policy and locks.
`specverb-gen` materializes the generated `main.go`, `go.mod`/`go.sum`, and the
binary out-of-band in its cache (the uv-venv analog), so the AWS SDK never
enters production ward's module and the parent `go build ./...` skips it.

## What mounts

`specverb.Mount` generates the whole tree from the Guardfile plus the pruned
spec - no hand-written commands. The forgejo surface spans `repo`, `org`,
`org-label`, `milestone`, `issue`, `release`, `issue-label`, `tasks`, and `pr`:

```
ward-kdl ops forgejo org get <org>   # GET /orgs/{org}
```

Each leaf carries `--dry-run`, `--query`, and `--output`. Hardened per
[ward#109](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/109):
`repo fork`/`archive`/`unarchive`, `org create`/`delete`, `issue delete`, and
the `pr` leaves are **denied** (each teaches why on invocation); label CRUD
targets org-labels; cross-repo `issue search`; `issue list-all`; and
`move-issue` (copy, back-link, close). `restrict owner matches "coily*"` scopes
every {owner} leaf. The complex actions are `issue list-all` (collect),
`move-issue` (call sequence), and `issue view` (issue + comments).

## No hand-written Go

`.ward/ward-kdl/` commits one Guardfile per API: `ward-kdl.forgejo.guardfile.kdl`
and the `trello`/`tailscale` spec guardfiles, plus `ward-kdl.aws.guardfile.kdl`
(exec). Each spec API commits a pruned lock (`*.swagger.lock.json` for the
Forgejo Swagger 2.0, `*.openapi.lock.json` for the Trello/Tailscale OpenAPI 3.x);
all share the `specverb.lock`. The `main.go`, `go.mod`/`go.sum`, and binary
materialize out-of-band; the REF pins a release. Build:

```
make build-ward-kdl   # specverb-gen lock (fetch + prune), then build -> bin/ward-kdl
```

## The spec lock (uv-style)

Following uv's model, a committed lock decouples the CLI's cadence from
upstream's. The lock is `go:embed`'d, so a normal run is **hermetic**, and is
**pruned to the granted operations + their transitive `$ref` schemas** (the
consumer's contract, not the full dump). `make lock` (re)builds it: fetch
Forgejo's live `/swagger.v1.json`, read the vendored Trello/Tailscale specs,
then prune each. `make skew` warns (kubectl-style) on drift.

## Merging multiple APIs

`specverb-gen` merges every `ward-kdl.<api>.guardfile.kdl` sharing the `wrap
ward-kdl` name into one binary, each `ops <api>` its own group, across
transports: forgejo/trello/tailscale (spec) and aws (exec) ship as one binary.
Each spec API gets a lock + reference doc; an exec API a reference doc only.
The generated `mountOps` dispatches per transport - spec via `specverb.Mount`
(Guardfile + spec + SSM `TokenResolver`), exec via `execverb.Mount` - all over
one `verb.Wrap` writing `~/.ward/audit/<slug>.jsonl`.

## Proving against the coily oracle

`ward-kdl ops forgejo repo create --name probe --dry-run --output json` prints
the resolved `POST /api/v1/user/repos` request (auth header redacted). Trello and
Tailscale prove the other auth schemes: `trello card create --name x --dry-run`
shows `?key=<redacted>&token=<redacted>`, and `tailscale policy get coilysiren.me
--dry-run` shows the `Bearer <redacted>` header. The engine is unit-tested in
cli-guard; ward-kdl carries no Go to test.

## See also

- [FEATURES.md](FEATURES.md) - ward feature inventory.
- [ops-forgejo-in-ward.md](ops-forgejo-in-ward.md) - the in-ward `ward ops forgejo` mount.
- cli-guard `specverb` - the engine ([cli-guard#75](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/issues/75)).
