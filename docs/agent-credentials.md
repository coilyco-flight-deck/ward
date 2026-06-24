# ward agent: credentials (claude, codex)

How ward seeds each harness's host credential into the container. The
local-model harnesses (qwen, goose) need no host credential - see
[docs/agent-local-harnesses.md](agent-local-harnesses.md).

## claude

claude runs **non-root** (uid 1000): it refuses `--dangerously-skip-permissions`
as root, so the entrypoint sets up as root then drops via `setpriv`. It
authenticates with your **Max/subscription login**, not an API key - ward
resolves the OAuth credential on the host and injects it into the container's
`~/.claude/.credentials.json` via the private `--env-file`, never in argv/audit.
`ANTHROPIC_API_KEY` stays unset so it can't shadow OAuth.

**Auth smoke test (ward#222).** A headless claude whose seeded credential cannot
authenticate emits zero output and either exits 0 or blocks forever, so the
container idles indefinitely while looking busy. Before launching the real agent
the entrypoint probes `claude -p` once, as the agent user, bounded (90s) and with
`/dev/null` stdin; on timeout or empty output it **aborts the container with a
clear error** (the reaper still runs) instead of letting it silently hang. The
host-side resolver also warns when the resolved blob is empty, has no access
token, or is expired (`re-run 'claude' on the host to refresh`). The probe is
headless-claude only; set `WARD_SMOKE_TEST_SKIP=1` to bypass it.

**Disk-aware diagnostics (ward#273).** A full Docker disk hangs claude startup
the same way a bad credential does (it cannot write `~/.claude`), so the smoke
test no longer blames the (valid) login on every stall. It pre-flight checks
free space on `/` and `/workspace` against a 512MiB floor and warns before the
90s wait, reports actual `df` headroom in the timeout and non-auth failure
messages, and only suggests re-login when the probe surfaces a genuine auth
marker (`401`, `Not logged in`, `invalid api key`, ...). The Go port
(`WARD_USE_GO_BOOTSTRAP=1`) and the shell entrypoint carry the same split. One-shot launches
(headless/ask) additionally pin agent stdin to `/dev/null` so a wedged agent gets
EOF and exits rather than blocking on an open pipe.

## codex

**codex** (ward#178) follows the same shape. ward resolves the host's
`~/.codex/auth.json` (the `codex login` blob - ChatGPT login or API key) and
injects it into the container's `~/.codex/auth.json` over the same private
`--env-file`, never in argv/audit; an absent host file just leaves codex
unauthenticated rather than failing the launch. Because the container is the
isolation boundary (like claude's `bypassPermissions` here), the entrypoint also
writes `~/.codex/config.toml` with `approval_policy = "never"` and
`sandbox_mode = "danger-full-access"`, so codex carries the issue - edit, commit,
push - without per-command approval prompts. Headless runs `codex exec <seed>`;
interactive `work` opens a seeded `codex` TUI. The host pre-flight one-shot is not
wired for codex yet, so the GO/NO-GO read bows out and dispatch proceeds.

## See also

- [docs/agent-local-harnesses.md](agent-local-harnesses.md) - qwen and goose (local models).
- [docs/agent.md](agent.md) - the `ward agent` verb family and usage.
