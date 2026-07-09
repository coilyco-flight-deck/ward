---
doc_goal: Make a reader trust that a headless agent run is fully reconstructable after teardown, and grasp that the drain keeps local archive artifacts plus a redacted sibling tree while the optional OTLP/SigNoz export stays deferred.
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
concatenated), and a small `meta.json`. The local archive writes those artifacts
to disk. Live tailing is separate.

`meta.json` is small and **secret-free** regardless of sink: `container`, `repo`,
`issue`, `driver`, `branch`, `outcome`. The dims come from the container's env
through a **strict allowlist** - `Config.Env` also carries the `--env-file` secrets
(`FORGEJO_TOKEN`, `WARD_CLAUDE_CREDS_B64`), never copied out; a test guards it.

## Local archive

Each run lands under `~/.ward/agent-logs/<container>/` as `console.log`,
`transcript.jsonl`, `meta.json` - mirroring the `~/.ward/audit/<slug>.jsonl`
convention ([audit.md](audit.md)): local, raw, ages out on its own.

It also writes a scrubbed sibling into a parallel `agent-logs-redacted/` tree that
the [director surface](agent-surface-log-read.md) binds.

### The redacted sibling

The redacted tree keeps the same safety boundary as the local archive: the console
is scrubbed line-for-line, and the transcript is reduced to tool-call envelopes
with body-shaped inputs dropped and args scrubbed through the shared regex list
(AWS / GitHub / Anthropic / JWT / public IP).

### Deferred OTLP

The SigNoz/OTLP export and dashboard template are deferred. The redaction helpers
stay because they feed the redacted on-disk view and keep the future export shape
available without shipping the network sink today.

## See also

- [container-lifecycle-logs.md](container-lifecycle-logs.md) - console marker conventions + debug flow.
- [container-cleanup.md](container-cleanup.md) - the keep-10 sweep this rides in.
- [audit.md](audit.md) - the `~/.ward/` layout the local archive mirrors.
