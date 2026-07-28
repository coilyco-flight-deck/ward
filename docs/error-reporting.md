# error reporting

Ward can send top-level Go panics to GlitchTip when `WARD_SENTRY_DSN` is set.

- It is off by default.
- It redacts before send.
- it preserves the panic after flush so ward still fails loudly.

## Scope

- top-level ward process panics only.
- host-side crash telemetry only.
- not agent-run reporting.

## Why it matters

The goal is to capture the crash without hiding it. If ward panics, it should
still fail loudly after the report flushes.

## See also

- [troubleshooting.md](troubleshooting.md) - what to read when a run fails.
