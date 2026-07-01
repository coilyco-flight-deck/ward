# ward agent: shared credentials

How ward seeds each harness's host credential into the container. The
per-agent specifics live in the harness pages.

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

**Env scrub after seeding (ward#357).** `WARD_CLAUDE_CREDS_B64` /
`WARD_CODEX_AUTH_B64` are bootstrap-only: the entrypoint (and the Go bootstrap)
`unset`s each after decoding it to its mode-600 file, mirroring the
git-credential scrub, so the live OAuth token can't leak on an `env` dump. Auth
still works - harnesses read it.

## codex

**codex** (ward#178) follows the same shape: ward injects the host's
`~/.codex/auth.json` (`codex login` - ChatGPT login or API key) over the private
`--env-file`, never in argv/audit; an absent file leaves codex unauthenticated.
Because the container is the isolation boundary, the entrypoint writes
`~/.codex/config.toml` with `approval_policy = "never"` and
`sandbox_mode = "danger-full-access"`, so codex works unprompted. That config also
pins the **cheapest codex posture by default** (ward#379): mini model, low
reasoning effort, low verbosity - the least usage per run, each overridable via
`WARD_CODEX_MODEL`, `WARD_CODEX_REASONING_EFFORT`,
`WARD_CODEX_VERBOSITY`.

Headless runs `codex exec <seed>`; interactive `work` opens a seeded `codex` TUI.
The GO/NO-GO pre-flight is not wired for codex yet, so dispatch proceeds.

## forgejo git auth

The container pushes over git-over-HTTPS with the bot `FORGEJO_TOKEN`, written to
`/etc/ward-git-credentials` and wired as git's `store` helper. Setup is root, then
the agent drops to non-root, so the file must be group-readable by the agent gid
(`0640 root:<agent-gid>`) for the push to use the bot credential.

**The clobber (ward#288).** git's `store` helper rewrites it to `0600 root:root`
on each successful auth, so the root-phase clones strip the group-read perms. An
unreadable file then sends the push down git's env fallback (`FORGEJO_TOKEN`) -
attributing the merge to `coilysiren`, not the bot. The bootstrap re-asserts the
perms before the drop and fails loud if the agent still cannot read it.

## See also

- [docs/agent-claude.md](agent-claude.md) - claude harness details.
- [docs/agent-codex.md](agent-codex.md) - codex harness details.
- [docs/agent-host-net.md](agent-host-net.md) - `--tailnet`, the tailnet route.
- [docs/agent.md](agent.md) - the `ward agent` verb family and usage.
