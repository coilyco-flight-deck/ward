# `ward ops forgejo` (in-binary mount)

ward#92 cut the `ward` binary over to the ward-kdl forgejo guardfile: `ward ops
forgejo <verb>` mounts the full 42-leaf `specverb` surface directly in the
shipped binary, alongside the out-of-band [`ward-kdl`](ops-forgejo.md). Every
Forgejo call ward makes - `ward agent`, the container reaper - routes through
this mount; the old hand-rolled client is retired (see below).

The forgejo guardfile is pure `specverb` (HTTP/REST), so it carries no AWS SDK
and folds into ward's normal `go build` cleanly - unlike the exec-transport
surfaces that keep ward-kdl out-of-band. `cmd/ward/ops.go` parses the embedded
guardfile + pruned spec lock and `specverb.Build`s the `forgejo` group under a
new `ops` umbrella, wrapping every leaf through ward's audit pipeline. The bot
token (`value ssm`) resolves through ward's audited `aws ssm` runner, not the
AWS SDK, and lazily - mount and `--dry-run` never touch SSM.

`go:embed` cannot reach a sibling directory, so `cmd/ward/opsassets/` holds
byte-for-byte copies of `cmd/ward-kdl/`'s canonical guardfile + spec lock. The
ward-kdl files stay the single source of truth (`make build-ward-kdl` re-runs
`make sync-ops-assets`); `cmd/ward/opsassets_test.go` fails the build on drift.

## `forgejo_issue.go` is retired (ward#92)

ward's hand-rolled Forgejo client is gone. Everything `ward agent` and the
container reaper need - issue/comment reads, issue/comment writes, close, the
salvage append, and the route survey - now routes through this mount via
`cmd/ward/forgejo_ops.go`, a thin client that shells the ward binary back to its
own `ops forgejo` leaves. The three seams that once blocked the cut all resolved
in the runtime (cli-guard v0.44.0):

- **Rich bodies ride `--body-file`.** Programmatic writes (salvage reports,
  reservation/pre-flight comments) are markdown full of backticks, `$`, and
  newlines that the argv shell-metacharacter gate refuses inline. `forgejo_ops.go`
  signs the body, writes it to a temp JSON file, and passes `--body-file <path>`;
  the path carries no metacharacters and the body never touches the gate.
- **One auth seam covers host and container.** The guardfile's `value ssm` address
  resolves through `forgejoTokenResolver` (`ops.go`): the baked `$FORGEJO_TOKEN`
  inside a container (the reaper has no AWS/SSM), else the coilyco-ops bot token
  from SSM on a host. The reaper drives the same client the host flows do.
- **Reads capture `--output json`.** Each read leaf renders its response body to
  stdout; `forgejo_ops.go` captures it and `json.Unmarshal`s the `Issue` /
  comment / repo structs back in Go - no new cli-guard API, just the rendered
  body plus a decode.

The survey's owner-repo listing needed two read leaves the guardfile did not yet
grant, so ward#92 grew it by two: `org-repo list` (GET /orgs/{org}/repos) and
`user-repo list` (GET /users/{username}/repos), the org and user halves the
catalog walks across the primary owners (the coilyco-* orgs and the coilysiren
user). The forgejo surface is now 42 leaves.

The stale-ward release-tag scalar read had already moved onto this mount ahead of
the rest (ward#172): `ops forgejo release list <owner> <repo> --query
"[0].tag_name" --output text` (see
[agent-reservation.md](agent-reservation.md#host-stale-ward-reminder-ward143)).

## See also

- [ops-forgejo.md](ops-forgejo.md) - the ward-kdl proving ground + guardfile.
- [container-reap.md](container-reap.md) - the reaper's salvage-issue seam, now on this mount.
