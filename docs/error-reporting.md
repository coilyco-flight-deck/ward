---
doc_goal: Describe ward's host-side GlitchTip crash reporting as a narrow, off-by-default panic-only surface so readers know exactly what is emitted, which env var turns it on, and where the boundary stops.
---
# Error reporting

ward can report **its own top-level Go panics** to the self-hosted, Sentry-compatible
**GlitchTip** backend through `github.com/getsentry/sentry-go`.

## What it covers

- Host-side `ward` process panics only.
- Release/version, invoked verb, hostname, and other non-secret runtime context.
- Secret scrubbing through the same `redactSecrets` discipline used elsewhere in ward.

## Off by default

The reporter stays disabled unless the operator exports:

- `WARD_SENTRY_DSN` - the GlitchTip DSN, resolved outside ward from `/sentry-dsn/ward`.
- `WARD_SENTRY_ENVIRONMENT` - optional deployment tag.

No DSN means no client, no network dependency, and no behavior change.

## What it does not cover

- No DSN injection into warded containers.
- No agent outcomes, transcripts, issue bodies, or run content.
- No replacement for the SigNoz agent-run telemetry path.
- No salvage-branch replay or broader crash surfaces yet.

The shape is deliberate: panic, report, flush, then re-panic so ward still exits loudly.

## See also

- [agent-observability.md](agent-observability.md) - the separate SigNoz agent-run surface.

