# ward-kdl agents goose

Exec-dialect CLI. Every verb runs `goose` with the granted subcommand (or its `argv` override) appended; the binary and its prefix are fixed and the caller can never substitute them.

## ward-kdl agents goose launch - interactive chat session

`goose session`

Flags: unrestricted passthrough.

## ward-kdl agents goose headless - execute commands from -t <text>, -i <file>, or stdin

`goose run`

Flags: unrestricted passthrough.

## See also

- [ward-kdl.md](../ward-kdl.md) - the build-time authoring layer behind this surface
- [ward-kdl-surface.md](../ward-kdl-surface.md) - the full generated verb surface, area by area
