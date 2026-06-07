# ward-kdl (spec-driven verb proving ground)

`ward-kdl` is a **temporary** parallel CLI that proves cli-guard's `specverb`
engine against the real Forgejo API before the spec-driven path folds into
`ward` proper ([ward#62](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/62)).
It is not shipped or installed; build and run it locally.

It is its **own isolated Go module** (`cmd/ward-kdl/go.mod`), so the dev-loop
`replace` and the AWS SDK never enter production ward's module - ward's build,
deps, and CI are untouched. The parent `go build ./...` skips the nested module.

## What mounts

```
ward-kdl ops forgejo repo get    <owner> <repo>   # GET    /repos/{owner}/{repo}
ward-kdl ops forgejo repo create --name <n> [...] # POST   /user/repos
ward-kdl ops forgejo repo delete <owner> <repo>   # DELETE /repos/{owner}/{repo}
```

The whole `ops forgejo repo ...` path is generated at runtime by
`specverb.Mount` from `cmd/ward-kdl/forgejo.guardfile.kdl` plus the spec - no
hand-written command tree. Each leaf carries `--dry-run` (print the resolved
request, auth header redacted), `--query`, and `--output`.

## No hand-written Go

`cmd/ward-kdl/` commits only `forgejo.guardfile.kdl` plus the module plumbing
(`go.mod`/`go.sum`, which carry the dev `replace`). There is **no hand-written
Go**: `main.go` is generated from the Guardfile by cli-guard's `specverb-gen`
(the SSM/AWS-SDK resolver, audit `Wrap`, spec resolution, and `Mount` call),
carries a `// Code generated ... DO NOT EDIT.` header, and is gitignored. Build:

```
make ward-kdl   # specverb-gen -> main.go, then go build -o bin/ward-kdl
```

## The spec is never committed

The 800KB Forgejo `swagger.v1.json` is a vendor artifact, kept out of git
(`*.swagger.v1.json` is gitignored). `ward-kdl` resolves it at runtime:

1. `$WARD_KDL_SPEC`, if set, names the spec file; else
2. `<user-cache>/ward-kdl/forgejo.swagger.v1.json`; on a cache miss it is
   fetched once from `https://forgejo.coilysiren.me/swagger.v1.json`.

## Seams

The generated `mountOps` wires the specverb seams: the embedded Guardfile, the
runtime spec bytes, `verb.Wrap` over the shared `~/.coily/audit/<slug>.jsonl`
writer, and the real SSM `TokenResolver` over the AWS SDK (resolving
`/forgejo/api-token`). The engine defaults the `https://` scheme and a
redirect-refusing client for mutating verbs.

## Proving against the coily oracle

For a create in the authenticated user's namespace both produce the same call:

```
$ ward-kdl ops forgejo repo create --name probe --private --dry-run --output json
{ "method": "POST",
  "url": "https://forgejo.coilysiren.me/api/v1/user/repos",
  "headers": { "Authorization": "token <redacted>", "Content-Type": "application/json" },
  "body": { "name": "probe", "private": true } }
```

Drop `--dry-run` for the live call (create, then `repo delete <owner> <repo>`
to self-clean); it needs AWS creds + network. The engine is unit-tested in
cli-guard (`specverb`, `specgen`); ward-kdl itself carries no Go to test.

## Dev-loop dependency

specverb is unreleased, so `cmd/ward-kdl/go.mod` carries a `replace` onto the
local cli-guard checkout (`../../../cli-guard`). Because this lives in the
isolated module, production ward never sees it. The whole module is dropped when
the spec-driven path folds into ward proper (#62).

## See also

- [FEATURES.md](FEATURES.md) - ward feature inventory.
- cli-guard `specverb` - the engine ([cli-guard#75](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/issues/75)).
