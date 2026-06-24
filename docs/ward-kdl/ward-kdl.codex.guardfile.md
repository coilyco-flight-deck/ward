# ward-kdl agents codex

Exec-dialect CLI. Every verb runs `codex` with the granted subcommand (or its `argv` override) appended; the binary and its prefix are fixed and the caller can never substitute them.

## ward-kdl agents codex launch - interactive session (bare codex)

`codex`

Flags: unrestricted passthrough.

## ward-kdl agents codex headless - run codex non-interactively; pass the prompt as args

`codex exec`

Flags: unrestricted passthrough.

## ward-kdl agents codex login - manage login

`codex login`

Flags: unrestricted passthrough.

## ward-kdl agents codex whoami - show login status

`codex login status`

Flags: unrestricted passthrough.
