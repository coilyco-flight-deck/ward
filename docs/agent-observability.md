---
doc_goal: Make a reader trust that a headless agent run is fully reconstructable after teardown, and grasp that the drain is a deliberate safety system - the endpoint-locality gate deciding full-vs-redacted content is load-bearing, not a config detail.
---
# agent-run observability

A headless `ward agent` run used to be unqueryable after it finished: its console
lived only in Docker's driver (gone with the keep-10 `docker rm`), and the claude
**transcript** (`~/.claude/projects/**/*.jsonl`) died with the container.

## The drain - host-native, shortly after exit

The [reaper](container-reap.md) runs **inside** the container with no docker
socket, so the drain is **host-side**, at two points sharing an idempotency marker
([drain-timing.md](drain-timing.md)): a detached waiter draining the moment a
container exits, and the keep-10 [sweep](container-cleanup.md) draining
every exited run **before** the `rm` takes its log.

Every drain pulls three things **into memory**: the console (`docker logs`,
stdout+stderr merged), the transcript (`docker cp`'d out of the projects tree and
concatenated), and a small `meta.json`. The transcript is `docker cp`'d out into
the local archive and the redacted sibling tree. Live tailing is separate.

`meta.json` is small and **secret-free** regardless of sink: `container`, `repo`,
`issue`, `driver`, `branch`, `outcome`. The dims come from the container's env
through a **strict allowlist** - `Config.Env` also carries the `--env-file` secrets
(`FORGEJO_TOKEN`, `WARD_CLAUDE_CREDS_B64`), never copied out; a test guards it.

## Sink modes

Where a drained run lands is the local disk archive. `WARD_AGENT_SINK` remains as
a compatibility knob, but `signoz` and `both` now collapse to `disk` while the
SigNoz export is deferred.

**logdy is retired.** The console now stays in the local archive, so reading a
drained run is a disk read.

### The disk sink

With `disk` (or `both`), each run lands under `~/.ward/agent-logs/<container>/` as
`console.log`, `transcript.jsonl`, `meta.json` - mirroring the
`~/.ward/audit/<slug>.jsonl` convention ([audit.md](audit.md)): local, raw, ages
out on its own.

It also writes a scrubbed sibling into a parallel `agent-logs-redacted/` tree that
the [director surface](agent-surface-log-read.md) binds.

### The deferred SigNoz sink

SigNoz export is parked for now. The redaction and envelope shaping stay in the
codebase because the local redacted archive still uses them, but no release path
ships the drained run off-box.

An envelope is call-metadata: tool, args, cwd, duration, pass/fail, lifecycle,
files touched. Redaction, when it applies, drops body-shaped inputs
(`content`, `new_string`, ...) and scrubs kept args through the regex list (AWS /
GitHub / Anthropic / JWT / public IP).

## See also

- [container-lifecycle-logs.md](container-lifecycle-logs.md) - console marker conventions + debug flow.
- [container-cleanup.md](container-cleanup.md) - the keep-10 sweep this rides in.
- [audit.md](audit.md) - the `~/.ward/` layout the disk archive mirrors.

## Deferred SigNoz dashboard

Deferred. The JSON template stays in-tree for the eventual export path, but the
release surface no longer ships a SigNoz sink.
