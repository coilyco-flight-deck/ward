# ward-kdl git

Exec-dialect CLI. Every verb runs `git` with the granted subcommand (or its `argv` override) appended; the binary and its prefix are fixed and the caller can never substitute them.

## ward-kdl git status - show the working tree state; -s for short format, -b for branch

`git status`

Flags: unrestricted passthrough.

## ward-kdl git log - commit history; --oneline, -p for patches, -n N to limit, --stat for a diffstat

`git log`

Flags: unrestricted passthrough.

## ward-kdl git show - show a commit/tag/blob; --stat, -p, or <rev>:<path> to dump a file at a rev

`git show`

Flags: unrestricted passthrough.

## ward-kdl git diff - working-tree/index/tree differences; --stat, --cached, or a <rev> range

`git diff`

Flags: unrestricted passthrough.

## ward-kdl git blame - line-by-line last-modified revision and author for a file

`git blame`

Flags: unrestricted passthrough.

## ward-kdl git describe - name a commit from the nearest tag; --tags, --dirty, --always

`git describe`

Flags: unrestricted passthrough.

## ward-kdl git shortlog - commit log grouped and counted by author; -s -n for a contributor leaderboard

`git shortlog`

Flags: unrestricted passthrough.
