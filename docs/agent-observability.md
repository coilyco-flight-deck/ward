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
concatenated), and a small `meta.json`. The drain writes the local archive by
default, so the transcript always lands under `~/.ward/agent-logs/` unless the
host write itself fails. Live tailing is separate.

`meta.json` is small and **secret-free** regardless of sink: `container`, `repo`,
`issue`, `driver`, `branch`, `outcome`. The dims come from the container's env
through a **strict allowlist** - `Config.Env` also carries the `--env-file` secrets
(`FORGEJO_TOKEN`, `WARD_CLAUDE_CREDS_B64`), never copied out; a test guards it.

## Sink modes

Where a drained run lands is controlled by `WARD_AGENT_SINK`, but the release
surface now uses it only as a label. The drain always writes the local disk
archive:

- **`disk`** *(default)* - write the on-disk artifacts and the redacted sibling.
- **`signoz`** - legacy compatibility label, ignored by routing.
- **`both`** - legacy compatibility label, ignored by routing.

`WARD_AGENT_SINK` stays as the operator-local seam for a later observability
relaunch. Any other value is accepted as a label and still lands in the local
archive.

**logdy is retired.** The console is archived locally and the director reads the
redacted sibling tree.

### The disk sink

With `disk`, each run lands under `~/.ward/agent-logs/<container>/` as
`console.log`, `transcript.jsonl`, `meta.json` - mirroring the
`~/.ward/audit/<slug>.jsonl` convention ([audit.md](audit.md)): local, raw, ages
out on its own.

It also writes a scrubbed sibling into a parallel `agent-logs-redacted/` tree that
the [director surface](agent-surface-log-read.md) binds.

### The redaction split - the safety crux

The fleet's secret-redaction scrubs the **terminal**, not the transcript jsonl, so
the redacted archive matters even without shipping anywhere else. The same
extractor drops body-shaped inputs (`content`, `new_string`, ...) and scrubs kept
args through the regex list (AWS / GitHub / Anthropic / JWT / public IP). That
keeps the local archive safe without depending on an external sink.

## See also

- [container-lifecycle-logs.md](container-lifecycle-logs.md) - console marker conventions + debug flow.
- [container-cleanup.md](container-cleanup.md) - the keep-10 sweep this rides in.
- [audit.md](audit.md) - the `~/.ward/` layout the disk sink mirrors.

## Deferred SigNoz dashboard

[`docs/ward-agent-reaps-dashboard.json`](ward-agent-reaps-dashboard.json) stays in
tree as the future SigNoz import body, but the release surface no longer ships run
logs to SigNoz. The template is deferred with the rest of the optional sink.
