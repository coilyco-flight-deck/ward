# ward-kdl agents claude

Exec-dialect CLI. Every verb runs `claude` with the granted subcommand (or its `argv` override) appended; the binary and its prefix are fixed and the caller can never substitute them.

## ward-kdl agents claude launch - interactive session (bare claude)

`claude`

Flags: unrestricted passthrough.

## ward-kdl agents claude headless - non-interactive print mode; pass the prompt as args

`claude -p`

Flags: unrestricted passthrough.

## ward-kdl agents claude login - manage authentication

`claude auth`

Flags: unrestricted passthrough.

## ward-kdl agents claude doctor - check Claude Code health

`claude doctor`

Flags: unrestricted passthrough.
