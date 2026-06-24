# `ward ops forgejo {admin,doctor}` (remote-exec slice)

ward#81 closes the last non-HTTP slice of the 40-leaf forgejo operator surface
(ward#75). Forgejo's server-side maintenance lives in the `forgejo` binary
*inside the cluster*, not in the REST API, so four subcommands have no `specverb`
equivalent:

- `ward ops forgejo admin user list`
- `ward ops forgejo admin user create`
- `ward ops forgejo admin auth list`
- `ward ops forgejo doctor check`

These ride cli-guard's **exec dialect** ([execverb](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/issues/130))
instead of HTTP: the wrapped binary is `ssh`, and an unoverridable `argv-prefix`
pins the whole remote-exec transport ahead of the subcommand:

```
ssh kai@kai-server k3s kubectl -n forgejo exec deploy/forgejo -- forgejo <subcommand> <args...>
```

The caller can never substitute the binary (`ssh`), the host (`kai@kai-server`),
the namespace (`forgejo`), the pod (`deploy/forgejo`), or the remote entrypoint
(`forgejo`) - only the granted subcommand and its caller args ride after the
`--`. Deny-by-default: only the four leaves above mount; everything else is
refused by the absence of a grant.

## Policy

- **`admin user create`** takes its flags (`--username` / `--email` /
  `--password` / `--admin` / ...) through unrestricted - the transport and
  entrypoint stay fixed, so the only freedom is the user being created.
- **`doctor check`** denies `--fix`, the one *mutating* flag on the subcommand,
  so a doctor leaf can diagnose but never repair. The benign selection flags
  (`--run` / `--all` / `--list` / `--log-file`) and `--help` still pass through.
  The sibling mutating subcommand (`doctor recreate-table`) is simply never
  granted.

## Mounting

The exec slice is a **ward-proper-only library mount**. `cmd/ward/ops.go`
parses the embedded `opsassets/forgejo-admin.guardfile.kdl`, `execverb.Build`s
its group, and grafts the built `admin` and `doctor` subtrees onto the same
`forgejo` command the `specverb` REST surface mounts (the guardfile's `wrap`
path ends in `forgejo` precisely so its children land as siblings of the REST
resources). REST and remote-exec thus share one `ops forgejo` command, two
transports under one operator verb - the [mixed-transports](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/raw/branch/main/docs/specverb-mixed-transports.md)
shape, composed in ward's own Go rather than the driver's generated `main`.

Every leaf wraps through ward's audit pipeline like every other verb, so each
remote-exec call writes one JSONL audit row (`ward.ops.forgejo.admin.user.list`
and friends). The exec dialect carries no upstream spec, so this slice embeds
only its policy guardfile - no spec lock, no SSM token (the wrapped `ssh`/`forgejo`
own their own credentials).

The guardfile lives directly under `cmd/ward/opsassets/` (not mirrored from
`cmd/ward-kdl/` like the REST guardfile) because it has no ward-kdl driver
counterpart yet: it declares `wrap ward ops forgejo`, not `wrap ward-kdl ...`, so
the driver does not discover it. Teaching the no-code `ward-kdl` driver to carry
this remote-exec group is the follow-up; until then ward proper carries it
through the library mount.

## See also

- [ops-forgejo-in-ward.md](ops-forgejo-in-ward.md) - the in-binary `specverb`
  forgejo mount this slice grafts onto.
- [ops-forgejo.md](ops-forgejo.md) - the ward-kdl proving ground + REST guardfile.
- [ward-kdl.kubectl.guardfile.md](ward-kdl.kubectl.guardfile.md) - the sibling
  `execverb` kubectl surface.
