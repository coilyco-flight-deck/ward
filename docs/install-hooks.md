# install-hooks verb

`ward install-hooks` idempotently registers the ward
PreToolUse hook in the consumer repo's `.claude/settings.json`.
Designed to be safe to re-run: existing unrelated entries are
preserved, and re-installs are no-ops.

## Target discovery

Default: `git rev-parse --show-toplevel` of cwd, plus
`/.claude/settings.json`. On failure (cwd is not in a git repo), exits
with an error and suggests `--path`.

Override: `--path <file>` names the `settings.json` directly.

## loadSettings

Reads the `settings.json` at path, returning a generic map. Missing
file returns an empty map (fresh install). Malformed JSON returns an
error: the caller refuses to clobber a file it cannot read.

## ensureHook merge rules

Returns `(present, merged)`. `present` is true if the wanted hook was
already registered (no merge needed). `merged` is the desired settings
map after ensuring the hook entry exists.

- `hooks` key missing: add as a map.
- `hooks.PreToolUse` missing: add as a list with our entry.
- `PreToolUse` has a `matcher="Bash"` entry: ensure its hooks list
  contains our command. Append if not present.
- No `matcher="Bash"` entry: append a new one.

Unknown keys at any level are preserved.

## cloneMap

Shallow-clones a `map[string]any` so we don't mutate the caller's
value while merging. Nested map / slice values are not deeply cloned
because `ensureHook` only mutates the top-level `hooks` key, but we do
clone the hooks subtree it touches.

## marshalSettings

Emits the settings map with two-space indent and a trailing newline.
`json.MarshalIndent` emits map keys in sorted order at every level,
matching the shape `coily lockdown` already writes so manual diffs
read cleanly.
