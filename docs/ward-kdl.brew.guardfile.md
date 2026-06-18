# ward-kdl pkg brew

Exec-dialect CLI. Every verb runs `brew` with the granted subcommand (or its `argv` override) appended; the binary and its prefix are fixed and the caller can never substitute them.

## ward-kdl pkg brew search - search formulae/casks by name or regex

`brew search`

Flags: unrestricted passthrough.

## ward-kdl pkg brew info - show formula/cask metadata; pass the name

`brew info`

Flags: unrestricted passthrough.

## ward-kdl pkg brew list - list installed formulae/casks

`brew list`

Flags: unrestricted passthrough.

## ward-kdl pkg brew deps - show a formula's dependency tree

`brew deps`

Flags: unrestricted passthrough.

## ward-kdl pkg brew leaves - list installed formulae not depended on

`brew leaves`

Flags: unrestricted passthrough.

## ward-kdl pkg brew outdated - list formulae with a newer version available

`brew outdated`

Flags: unrestricted passthrough.

## ward-kdl pkg brew doctor - diagnose common brew problems

`brew doctor`

Flags: unrestricted passthrough.

## ward-kdl pkg brew config - print brew's configuration

`brew config`

Flags: unrestricted passthrough.

## ward-kdl pkg brew commands - list available brew commands

`brew commands`

Flags: unrestricted passthrough.

## ward-kdl pkg brew services list - list managed services and their state

`brew services list`

Flags: unrestricted passthrough.

## ward-kdl pkg brew services info - detail a managed service; pass the name

`brew services info`

Flags: unrestricted passthrough.

## ward-kdl pkg brew update - refresh formulae definitions from the taps

`brew update`

Flags: unrestricted passthrough.

## ward-kdl pkg brew bundle - install from a Brewfile; pass --file to point at one

`brew bundle`

Flags: unrestricted passthrough.
