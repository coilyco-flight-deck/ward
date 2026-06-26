# agent-run observability

A headless `ward agent` run used to be unqueryable after it finished. Its console
stream lived only in Docker's `json-file` driver (gone with the keep-10
`docker rm`), and the claude **transcript** (`~/.claude/projects/**/*.jsonl`)
died with the container, never exported. ward#363 closes that, in two slices.

## Slice 1 - host-native drain on reap (always on)

The container [reaper](container-reap.md) runs **inside** the container with no
docker socket, so it can never reach `docker logs`. The drain is therefore
**host-side**, folded into the keep-10 stale-container sweep
([container-cleanup.md](container-cleanup.md)): right **before** the sweep
`docker rm`s an exited container past the keep window, ward drains it. Ordering is
the load-bearing contract - the `rm` takes the console log and the writable layer
(the transcript) with it, so the drain must come first. A pure `sweepActions`
planner makes that explicit, and a test asserts no remove precedes a drain.

Each drained run lands under `~/.ward/agent-logs/<container>/`:

- `console.log` - `docker logs`, stdout+stderr merged (agent stream + reaper markers).
- `transcript.jsonl` - the claude session jsonl, docker-cp'd out and concatenated.
- `meta.json` - run dims + outcome.

This mirrors the `~/.ward/audit/<slug>.jsonl` convention ([audit.md](audit.md)):
local, raw, never leaves the host, ages out on its own. Point Dozzle / `jq` at it.

`meta.json` is small and **secret-free**: `container`, `repo`, `issue`, `driver`,
`branch`, `outcome`. The `outcome` is inferred from the reaper's own console
markers (`landed on main` -> `pushed-to-main`, a `ward-salvage/` push ->
`ward-salvage`). The dims come from the container's inspected env, but **only
through a strict allowlist**: `Config.Env` also carries the `--env-file` secrets
(`FORGEJO_TOKEN`, `WARD_CLAUDE_CREDS_B64`), so the drain copies only the known-safe
dims and never the whole env. A unit test guards that boundary.

The drain is best-effort throughout: a missing transcript, a `docker inspect`
miss, an unwritable dir - each is logged and stepped past, never a launch failure.

## Slice 2 - redacted envelope stream to SigNoz (export defaults OFF)

The extractor turns a drained transcript into one **envelope per tool call** and
ships them to the fleet SigNoz OTLP endpoint (`http://ser8:4318/v1/logs`, override
via `WARD_AGENT_TELEMETRY_ENDPOINT`) as structured logs, **default-OFF** behind
`WARD_AGENT_TELEMETRY=1`. The host drain stays always-on.

An envelope is **call-metadata only**: tool name, redacted args, cwd, duration,
pass/fail, lifecycle step (clone / implement / merge / push), files touched, plus
run-level dims on the OTLP resource.

### The crux - redaction at extraction, before export

The fleet's Warp secret-redaction scrubs the **terminal**, not the transcript
jsonl, and SigNoz has **no ingest redaction processor**. So redaction is enforced
**here**, upstream of the sink, two ways:

1. **Bodies are dropped, not redacted.** Tool **results** carry the highest
   credential risk (a Read of a `.env`) and never enter an envelope. Body-shaped
   **inputs** (`content`, `new_string`, ...) drop the same way - only the file path
   they touched is kept.
2. **Args are redacted.** The args that do ride (a `Bash` command, a path) run
   through the Warp regex list (AWS / GitHub / Anthropic / Slack / JWT / public IP)
   before they enter an envelope, and are length-capped.

Per the deploy `log-schema.md` contract, bounded enums become indexed OTLP
attributes (`verb`, `outcome`, `lifecycle`, `duration_ms`, `repo`, `actor`,
`issue`); unbounded ids stay in the log `body`. Default-OFF because this defines a
first-of-pattern schema: nothing flows into the 90-day store until Kai has
reviewed the redaction. Extractor and shipper are built and unit-tested.

## See also

- [container-cleanup.md](container-cleanup.md) - the keep-10 sweep this rides in.
- [audit.md](audit.md) - the `~/.ward/` layout this mirrors.
