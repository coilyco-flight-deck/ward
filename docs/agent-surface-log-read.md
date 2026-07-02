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

Instead ward binds the host [agent-log drain](agent-observability.md)
(`~/.ward/agent-logs/`, `agentLogsDir()`) **read-only** at `/opt/ward-agent-logs` in the
surface session. A director reads any past run's drained `console.log` /
`transcript.jsonl` / `meta.json` there, with none of the socket's escalation surface, and
cannot write to the mount (`:ro`).

The mount is a `mountOpts.AgentLogsDir` opt-in on `leastAccessMounts`, set **only** on the
surface bring-up path. The shared least-access default never carries it, so the
engineer/advisor roles do not get it (a cross-run log read with far less justification).

## The residual: raw artifacts (default: accept)

The drained `console.log` and `transcript.jsonl` are **raw**: only the Slice-2 SigNoz
envelope export is redacted, not the on-disk artifacts. So a whole-dir read-only mount lets
a director read every past run's raw stdout + session, which can contain secrets that
leaked to stdout (a Read of a `.env`, a token in a traceback) and other runs' content.

This is accepted: these artifacts already live host-local unredacted, the same trust tier
as `~/.ward/audit/`, and read-only + no-socket removes the real escalation vector. A
tighter option, if wanted later, is to bind only the secret-free `meta.json`s.

## See also

- [docs/agent-surface.md](agent-surface.md) - the read-only surface this mount serves.
- [docs/agent-observability.md](agent-observability.md) - the host-side drain it exposes.
