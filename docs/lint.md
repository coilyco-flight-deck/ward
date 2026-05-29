# lint verb

`ward lint` validates the ward allowlist against the
repo's `Makefile` so the verb surface and the make-target surface
cannot drift.

## Rules

- `commands.<verb>.run` must equal `make <verb>`.
- The `Makefile` must declare a target named `<verb>`.
- The verb description must equal the Makefile target's `## desc`
  auto-help comment.
