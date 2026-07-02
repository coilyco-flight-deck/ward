# ward-kdl authoring

`ward-kdl` is the build-time authoring layer. See [cmd/ward-kdl/README.md](../cmd/ward-kdl/README.md).

- Dialect 1 - `*.guardfile.kdl` permission surfaces.
- Dialect 2 - `ward-kdl.fleet.kdl` embedded fleet config.
- Dialect 3 - `~/.ward/fleet.local.kdl` operator-local fleet config.

Source file in, cli-guard validates or compiles it, `ward` embeds the result,
and nothing is fetched at runtime.

## Getting the `ward-kdl` binary

`ward-kdl` is **not** a public install artifact - the brew formula installs only
`ward`, whose embedded surfaces already cover what an end user runs, so neither
the authoring binary nor the tier CLIs ship on the release page (ward#455). A
spec author who needs `ward-kdl` itself builds it from a ward checkout - one
documented path:

```
git clone https://forgejo.coilysiren.me/coilyco-flight-deck/ward
cd ward
make build-ward-kdl        # -> bin/ward-kdl (+ the read/write/admin tiers)
```

`make build-ward-kdl` drives the pinned `specverb-gen` generator (`REF` in the
[Makefile](../Makefile)) over the committed guardfiles - the same generator the
formula used to run inline before ward#455. It emits `bin/ward-kdl` plus the
`bin/ward-kdl-{read,write,admin}` tiers, stamped with the git-described version.
