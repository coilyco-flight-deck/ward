# install-hooks verb

`ward install-hooks` idempotently registers the ward
PreToolUse hook in the consumer repo's `.claude/settings.json`.
Designed to be safe to re-run: existing unrelated entries are
preserved, and re-installs are no-ops.

The merge mechanics live upstream in cli-guard's
[`cli/hook.Installer`](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard);
ward owns only the `install-hooks` command surface and the entry it
registers (`Matcher: "Bash"`, `Command: "ward hook pre-tool-use"`). See
[ward-kdl.md](ward-kdl.md) for the engine/generator/product split.

## Target discovery

Default: `git rev-parse --show-toplevel` of cwd, plus
`/.claude/settings.json` (cli-guard's `hook.ResolveSettingsPath`). On
failure (cwd is not in a git repo), exits with an error and suggests
`--path`.

Override: `--path <file>` names the `settings.json` directly.

## Settings I/O

`hook.LoadSettings` reads the `settings.json` at path, returning a generic
map. A missing file returns an empty map (fresh install). Malformed JSON
returns an error: the installer refuses to clobber a file it cannot read.
`hook.MarshalSettings` emits two-space-indented JSON with a trailing
newline, and `hook.WriteSettings` writes it atomically (tempfile + rename).

## Merge rules

`Installer.Ensure` returns `(present, merged)`. `present` is true if the
wanted hook was already registered (no merge needed). `merged` is a deep
copy of the settings map with the hook entry ensured:

- `hooks` key missing: add as a map.
- `hooks.PreToolUse` missing: add as a list with our entry.
- `PreToolUse` has a `matcher="Bash"` entry: ensure its hooks list
  contains our command. Append if not present.
- No `matcher="Bash"` entry: append a new one.

Unknown keys at any level are preserved.
