---
doc_goal: Explain how the director reads run logs without a docker socket - a read-only bind of the redacted drain tree - and why that indirection is a real security choice, since the socket is host-root-equivalent and the raw transcript carries unscrubbed secrets.
---
# Reading run logs from the director surface

The [director read-only surface](agent-surface.md) reads a run's logs **read-only**,
with no docker socket. This is the safe answer to "the director can't read run logs".

## Why not the socket

The surface deliberately has **no docker socket**: the socket is host-root-equivalent.
`docker inspect <sibling>` alone dumps `Config.Env` with every injected secret
(`FORGEJO_TOKEN`, `WARD_CLAUDE_CREDS_B64`, AWS/Anthropic creds), and `docker exec` is full
sibling takeover ([SECURITY.md](../SECURITY.md)). That is why dispatch goes over the TCP
broker, not the socket.

## The read-only bind ([ward#525](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/525))

Instead ward binds a host [agent-log drain](agent-observability.md) **read-only** at
`/opt/ward-agent-logs` in the surface session. A director reads past runs' drained logs +
`meta.json` there, with none of the socket's escalation surface, and cannot write to the
mount (`:ro`).

The mount is a `mountOpts.AgentLogsDir` opt-in on `leastAccessMounts`, set **only** on the
surface bring-up path. The shared least-access default never carries it, so the
engineer/advisor roles do not get it (a cross-run log read with far less justification).

## The bound source is the redacted view ([ward#526](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/526))

The mount binds the **redacted** drain tree (`~/.ward/agent-logs-redacted/`,
`agentLogsRedactedDir()`), **not** the raw `~/.ward/agent-logs/`. At drain time, whenever
the disk sink writes the raw `console.log` / `transcript.jsonl`, it also writes a redacted
sibling into the parallel tree:

- **`console.redacted.log`** - the console with every known secret shape scrubbed
  (`redactConsole`, reusing the extractor's `redactSecrets`).
- **`transcript.redacted.jsonl`** - the transcript reduced to bodies-dropped, args-scrubbed
  tool **envelopes**, one per line (`redactedTranscript`, reusing
  `extractEnvelopes(_, true)` - the exact redaction the remote SigNoz export runs).
- **`meta.json`** - already secret-free, copied over verbatim.

So the redaction is **shared with the envelope extractor**, not a second redactor: one
source of truth for what counts as a secret. A director gets readable console + transcript
views with tool-result bodies dropped and secret shapes scrubbed, and the raw artifacts -
plus the `dispatch/` logs that also live under `agent-logs/` - never reach the mount.

The raw `console.log` / `transcript.jsonl` stay on disk under `agent-logs/` for the
host-native drain + SigNoz path: this **adds** the redacted view, it does not replace the
raw archive.

### Requires the disk sink

The redacted view rides the same disk gate as the raw artifacts (`WARD_AGENT_SINK` =
`disk` or `both`; [agent-observability.md](agent-observability.md)). Under the
`signoz`-only default nothing is written to either tree, so the surface mount is empty -
turn on the disk sink for a director to have logs to read.

## See also

- [docs/agent-surface.md](agent-surface.md) - the read-only surface this mount serves.
- [docs/agent-observability.md](agent-observability.md) - the host-side drain it exposes.
