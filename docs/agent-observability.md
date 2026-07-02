# agent-run observability

A headless `ward agent` run used to be unqueryable after it finished: its console
lived only in Docker's driver (gone with the keep-10 `docker rm`), and the claude
**transcript** (`~/.claude/projects/**/*.jsonl`) died with the container. ward#363
opened the drain; ward#532 reshaped where it lands; ward#510 made it fire shortly
after exit, not only at eviction.

## The drain - host-native, shortly after exit

The [reaper](container-reap.md) runs **inside** the container with no docker
socket, so the drain is **host-side**, at two points sharing an idempotency marker
([drain-timing.md](drain-timing.md)): a detached waiter draining the moment a
container exits (ward#510), and the keep-10 [sweep](container-cleanup.md) draining
every exited run **before** the `rm` takes its log (ward#363).

Every drain pulls three things **into memory**: the console (`docker logs`,
stdout+stderr merged), the transcript (`docker cp`'d out of the projects tree and
concatenated), and a small `meta.json`. The transcript is `docker cp`'d out **even
in the signoz-exclusive default** - it never touches `~/.ward/agent-logs/` unless a
disk sink is on. Live tailing is separate.

`meta.json` is small and **secret-free** regardless of sink: `container`, `repo`,
`issue`, `driver`, `branch`, `outcome`. The dims come from the container's env
through a **strict allowlist** - `Config.Env` also carries the `--env-file` secrets
(`FORGEJO_TOKEN`, `WARD_CLAUDE_CREDS_B64`), never copied out; a test guards it.

## Sink modes (ward#532)

Where a drained run lands is a selectable sink, `WARD_AGENT_SINK`:

- **`signoz`** *(default)* - ship to SigNoz, persist **nothing** to disk.
- **`disk`** - write the on-disk artifacts, ship nothing.
- **`both`** - do both.

The default is **local-exclusive + full**: the whole run ships to the **local**
SigNoz (infrastructure#435; `http://localhost:4318` default, override
`WARD_AGENT_TELEMETRY_ENDPOINT`) and nothing is written to disk. `WARD_AGENT_SINK`
is the operator-local knob; `resolveSinkMode` is the seam a future ward-kdl config
field slots behind. An unrecognized value falls back.

**logdy is retired.** The console now ships to SigNoz as log records, so reading a
drained run is a SigNoz query.

### The disk sink

With `disk` (or `both`), each run lands under `~/.ward/agent-logs/<container>/` as
`console.log`, `transcript.jsonl`, `meta.json` - mirroring the
`~/.ward/audit/<slug>.jsonl` convention ([audit.md](audit.md)): local, raw, ages
out on its own.

It also writes a scrubbed sibling into a parallel `agent-logs-redacted/` tree that
the [director surface](agent-surface-log-read.md) binds (ward#526).

### The SigNoz sink and the locality gate - the safety crux

What ships to SigNoz is chosen by **endpoint locality**, the load-bearing safety
boundary. The fleet's secret-redaction scrubs the **terminal**, not the transcript
jsonl, and SigNoz has **no ingest redaction**. So **full, unredacted content may
go only to a local endpoint**:

- **Local (loopback)** - `localhost` or a loopback IP - gets the **full run**: the
  console as one log record per line (the logdy replacement) **plus** the
  transcript as per-tool-call **envelopes with bodies kept**, unredacted.
- **Remote (shared)** - ser8 or any non-loopback host - falls back to **redacted
  envelopes**: bodies dropped, args scrubbed. Full content never leaves the box. A
  test asserts remote => redacted / no bodies / no console, local => full allowed.

An envelope is otherwise call-metadata: tool, args, cwd, duration, pass/fail,
lifecycle, files touched. Redaction, when it applies, drops body-shaped inputs
(`content`, `new_string`, ...) and scrubs kept args through the regex list (AWS /
GitHub / Anthropic / JWT / public IP). Bounded enums become indexed OTLP attributes.

## See also

- [container-cleanup.md](container-cleanup.md) - the keep-10 sweep this rides in.
- [audit.md](audit.md) - the `~/.ward/` layout the disk sink mirrors.
