# `ward ops forgejo` (in-binary mount)

ward#92 cut the `ward` binary over to the ward-kdl forgejo guardfile: `ward ops
forgejo <verb>` mounts the full 40-leaf `specverb` surface directly in the
shipped binary, alongside the out-of-band [`ward-kdl`](ops-forgejo.md).

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

## Why `cmd/ward/forgejo_issue.go` stays

ward#92 also asked to retire `forgejo_issue.go` - ward's hand-rolled Forgejo
client behind `ward agent` and the container reaper. That retirement is
**blocked** on the runtime as it stands; the file is still load-bearing:

- **The argv gate rejects rich bodies.** Every wrapped verb runs its argv
  through cli-guard's shell-metacharacter policy. ward's programmatic writes
  (salvage reports, reservation/pre-flight comments) are markdown full of
  backticks, `$`, and newlines; routed as `--body` they are refused, and the
  guardfile exposes no file-based body input.
- **SSM-only auth vs. the no-SSM reaper.** The guardfile authenticates from SSM,
  but the in-container reaper (`container_reap.go`) files salvage issues with the
  baked `$FORGEJO_TOKEN` and has no AWS/SSM.
- **No programmatic capture.** `specverb` renders to stdout and surfaces non-2xx
  only as an error string, but the read seams (issue + comment fetch) need the
  decoded JSON *structs* back in Go.

Full retirement waits on a programmatic-capture API in cli-guard plus a
body-by-file / trusted-call path and a reaper auth seam - follow-up to ward#92.

### What did migrate: the scalar-read seam (ward#172)

The one read that *didn't* need a struct - the `ward agent` stale-ward check's
"latest ward release tag" lookup - moved off its own hand-rolled HTTP client and
onto this mount. A single scalar clears the capture blocker without a new API:
ward shells itself to `ops forgejo release list <owner> <repo> --query
"[0].tag_name" --output text` and reads the projected tag off stdout (see
[agent.md](agent.md#host-stale-ward-reminder-ward143)). `forgejo_issue.go` can't
follow suit - it needs whole `Issue`/comment objects, not a `--query` scalar, and
still hits the body-gate and reaper-auth blockers above.

## See also

- [ops-forgejo.md](ops-forgejo.md) - the ward-kdl proving ground + guardfile.
- [container-reap.md](container-reap.md) - a seam `forgejo_issue.go` still serves.
