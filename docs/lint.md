# lint verb (deprecated)

`ward lint` is a thin alias for [`ward doctor allowlist`](doctor.md). The
alias prints a one-line deprecation to stderr and delegates to the same
engine. It will be removed in a future minor release; consumers should
migrate to `ward doctor allowlist` (the cross-repo pre-commit suite is the
primary holdout).

The contract the engine enforces is unchanged:

- `commands.<verb>.run` must equal `make <verb>`.
- The `Makefile` must declare a target named `<verb>`.
- The verb description must equal the Makefile target's `## desc` auto-help comment.
