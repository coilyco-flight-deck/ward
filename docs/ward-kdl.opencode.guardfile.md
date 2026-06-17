# ward-kdl agents opencode

Exec-dialect CLI. Every verb runs `opencode` with the granted subcommand (or its `argv` override) appended; the binary and its prefix are fixed and the caller can never substitute them.

## ward-kdl agents opencode launch - interactive TUI (bare opencode)

`opencode`

Flags: unrestricted passthrough.

## ward-kdl agents opencode headless - run opencode with a message; pass it as args

`opencode run`

Flags: unrestricted passthrough.

## ward-kdl agents opencode login - log in to a provider

`opencode auth login`

Flags: unrestricted passthrough.

## ward-kdl agents opencode whoami - list providers and credentials

`opencode auth list`

Flags: unrestricted passthrough.
