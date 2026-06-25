# `ward ops forgejo` (in-binary mount)

ward#92 cut the `ward` binary over to the ward-kdl forgejo guardfile: `ward ops
forgejo <verb>` mounts the 42-leaf `specverb` surface directly in the
shipped binary, alongside the out-of-band [`ward-kdl`](ops-forgejo.md). Every
Forgejo call ward makes - `ward agent`, the container reaper - routes through
this mount (the old hand-rolled client is retired, below).

The forgejo guardfile is pure `specverb` (HTTP/REST), so it carries no AWS SDK
and folds into ward's normal `go build` cleanly. `cmd/ward/ops.go` parses the embedded
guardfile + pruned spec lock and `specverb.Build`s the `forgejo` group under a
new `ops` umbrella, re-rooted from `ward-kdl` to `ward` (ward#270) so its leaves
audit as `ward.ops.forgejo.*`, wrapping each through ward's audit pipeline. The bot
token (`value ssm`) resolves through ward's audited `aws ssm` runner, not the
AWS SDK, and lazily - mount and `--dry-run` never touch SSM.

`go:embed` cannot reach a sibling directory, so `cmd/ward/opsassets/` holds
`.generated.`-marked copies of `cmd/ward-kdl/`'s canonical guardfile + spec lock
(`opsassets/README.md`). The ward-kdl files stay the single source of
truth (`make build-ward-kdl` re-runs `make sync-ops-assets`);
`cmd/ward/opsassets_test.go` fails the build on drift.

## The remote-exec slice grafted alongside (ward#81)

The four server-side `forgejo` maintenance subcommands with no REST equivalent
(`admin user list/create`, `admin auth list`, `doctor check`) ride the
**exec dialect** instead, so `ward ops forgejo` now mounts both transports.
`graftForgejoAdminExec` (`cmd/ward/ops.go`) parses the embedded
`opsassets/forgejo-admin.guardfile.kdl`, `execverb.Build`s it, and appends the
built `admin`/`doctor` subtrees onto the same `forgejo` command - two transports
under one operator verb. That guardfile is ward-proper-only (no ward-kdl mirror,
no spec lock, no SSM token), so it lives directly under `opsassets/` and is
absent from the drift map. See [ops-forgejo-admin.md](ops-forgejo-admin.md).

## `forgejo_issue.go` is retired (ward#92)

ward's hand-rolled Forgejo client is gone. Everything `ward agent` and the
container reaper need - issue/comment reads, issue/comment writes, close, the
salvage append, and the route survey - now routes through this mount via
`cmd/ward/forgejo_ops.go`, a thin client that shells the ward binary back to its
own `ops forgejo` leaves. The three seams that once blocked the cut all resolved
in the runtime (cli-guard v0.44.0):

- **Rich bodies ride `--body-file`.** Markdown writes (salvage reports,
  reservation/pre-flight comments) carry backticks, `$`, and newlines the argv
  metachar gate refuses inline; `forgejo_ops.go` signs the body and passes it via
  a temp `--body-file`, so the body never touches the gate.
- **One auth seam covers host and container.** The guardfile's `value ssm` address
  resolves through `forgejoTokenResolver` (`ops.go`): the baked `$FORGEJO_TOKEN`
  in a container (no AWS/SSM), else the coilyco-ops bot token from SSM on a host.
- **Reads capture `--output json`.** Each read leaf renders its body to stdout;
  `forgejo_ops.go` captures and `json.Unmarshal`s the structs back in Go.

The survey's owner-repo listing grew the surface by two read leaves (now 42):
`org-repo list` (GET /orgs/{org}/repos) and `user-repo list`
(GET /users/{username}/repos) - the org and user halves of the primary owners
(the coilyco-* orgs and the coilysiren user).

The stale-ward release-tag scalar read had already moved onto this mount ahead of
the rest (ward#172): `ops forgejo release list <owner> <repo> --query
"[0].tag_name" --output text` (see
[agent-reservation.md](agent-reservation.md#host-stale-ward-reminder-ward143)).

## See also

- [ops-forgejo.md](ops-forgejo.md) - the ward-kdl proving ground + guardfile.
- [ops-forgejo-view.md](ops-forgejo-view.md) - the lean `issue view` override.
- [container-reap.md](container-reap.md) - the reaper's salvage-issue seam, now on this mount.
