# ward-kdl git

Exec-dialect CLI. Every verb runs `git` with the granted subcommand (or its `argv` override) appended; the binary and its prefix are fixed and the caller can never substitute them.

## ward-kdl git status - show the working tree state; -s for short format, -b for branch

`git status`

Flags: unrestricted passthrough.
