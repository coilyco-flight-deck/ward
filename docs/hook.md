# ward hook

`ward hook` groups Claude Code hook entry points. Subcommands
read a hook payload on stdin and either pass through (exit 0) or block
with a routing hint on stderr (exit 2).

Today this is wired only for PreToolUse on the Bash tool. The shape is
extensible to other hook events when there is a reason to gate on them.

## Inputs

`hookInput` is the subset of Claude Code's PreToolUse payload we read.
Unknown fields are ignored. `tool_input` is a free-form map so a
non-Bash tool name passes through cleanly.

## Protected-binary deny

When the resolved config (walk-up from the Claude-supplied `cwd`) declares
a `security:` block, ward feeds its `protected_binaries` and
`hooks.deny_bare_binaries` entries into cli-guard's engine as
`hook.Protected` values. The engine matches by basename, so a single
declaration covers:

- bare token: `gcloud auth login`
- absolute path: `/opt/homebrew/bin/gcloud auth login`
- relative path: `./bin/gcloud auth login`

Hint precedence: `hooks.route_hints[name]` when set, else the engine
synthesizes one from `allowed_wrappers`, else a bare deny.

The config load is best-effort. No config reachable, parse failure, or
malformed YAML all pass through silently — same posture as malformed
hook payloads. Hard denial stays the job of `permissions.deny`. See
`loadProtectedForCwd` / `protectedFor` in `cmd/ward/hook_protected.go`.

## Guard binary path check

`guardBinaryPaths` is the canonical install-path allow-list per known
guard binary. The PreToolUse hook rejects any bare invocation of one
of these binaries that does not resolve to a listed path. Required by
default per the max-security posture (#14). #13 carries the future
per-consumer override path.

## runPreToolUse

The testable core. Reads a hook payload, emits any block reason to
stderr, and returns nil on pass-through. Returns a `cli.Exit` error
with code 2 on block, which urfave/cli surfaces as the process exit
code.

Failure modes (unparseable JSON, missing fields, unknown tool, no
matching route) all pass through. The hook is a best-effort hint
surface, never a hard gate. coily lockdown / ward's own
`permissions.deny` stays responsible for hard denial.

## checkBinaryPath

Resolves token via lookup and returns a non-empty hijack-warning
string when the resolved path is outside the allowed list. `ENOENT`
returns `""` so bash surfaces the command-not-found error naturally.

Resolution uses lookup directly without canonicalizing symlinks since
`command -v` returns the symlink path (e.g. brew's
`/opt/homebrew/bin/coily` symlink). Matching the symlink is the
documented contract from coily's prior shell gate.

## detectGuard

Walks up from cwd for the nearest config marker and returns
`ward` or `coily`. Defaults to `ward` when no marker is
reachable so the hook still emits a usable hint in
contributor-cloning-a-ward-managed-repo contexts.

## splitSegments and stripEnvPrefix

`splitSegments` breaks a bash command into the leading-token segments
we want to classify. Mirrors the awk in coily's `lockdown-deny.sh`:
splits on `$( ) || && | ; &` boundaries. Imperfect (not a shell parser).

`stripEnvPrefix` peels leading `env VAR=val ...` and `sudo` tokens so
`env FOO=bar gh issue view` classifies the same as bare `gh issue view`.
Strips iteratively in case both env and sudo are present.

## Route tables

`coilyRoutes` and `wardRoutes` map a bare leading-token to a
recovery hint. ward's table is smaller: it wraps dev verbs, not
personal ops binaries.

`routeHint` returns the stderr block reason or `""` if the token has
no route. `isGhGraphQLSubcommand` returns true for gh subcommands that
route through GraphQL by default.
