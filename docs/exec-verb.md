# exec verb

The `agent-guard exec` verb walks up from cwd looking for an
agent-guard or coily allowlist, then exposes each declared command
as a leaf subcommand.

When no config is reachable, `exec` is still registered (so `--help`
and the `version` verb behave consistently) but every invocation
returns a clear error pointing at the missing file.

Each leaf runs the configured argv inside the repo that declared it,
after validating every argv token against cli-guard's
shell-metacharacter policy.
