# ward-kdl authoring

`ward-kdl` is the build-time authoring layer. See [cmd/ward-kdl/README.md](../cmd/ward-kdl/README.md).

- Dialect 1 - `*.guardfile.kdl` permission surfaces.
- Dialect 2 - `ward-kdl.fleet.kdl` embedded fleet config.
- Dialect 3 - `~/.ward/fleet.local.kdl` operator-local fleet config.

Source file in, cli-guard validates or compiles it, `ward` embeds the result,
and nothing is fetched at runtime.
