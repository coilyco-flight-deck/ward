# ward doctor

`ward doctor` is ward's diagnostic umbrella. It is the single verb a contributor or CI step calls to validate that the repo's `.ward/ward.yaml` and host posture are in shape. Every check group reads the resolved config path (`--config` > `$WARD_CONFIG` > walk-up from cwd, per [config-discovery](config-discovery.md)).

## Invocations

- `ward doctor` — runs every check group. Exits non-zero on any failure; partial summaries still flush so an operator can see what passed.
- `ward doctor allowlist` — validates `.ward/ward.yaml` (or `.coily/coily.yaml`) against the repo's `Makefile`. Same contract `ward lint` enforced; the alias still works for one minor release.
- `ward doctor security` — summarizes the parsed `security:` block from cli-guard's `repocfg`. Reports protected-binary count, sudo posture, and hook-policy presence. A config with no `security:` block is a pass and reports `no security: declared`.

ward parses but does not enforce the `security:` block. Enforcement lives in `ward doctor security`'s future host probes (passwordless sudo, PATH-shim detection, credential env scan) and in the PreToolUse hook's protected-binary wiring — both tracked as separate sub-issues of #4.

## Output

Each check group prints one summary line to stdout on success. On failure, the group's error is prefixed with the group name (`allowlist: …`, `security: …`) and surfaces on stderr via cli's standard error path. `ward doctor` aggregates failures across groups before returning, so the exit code reflects any failure even when later groups passed.

## Migration from `ward lint`

`ward lint` is a thin alias for `ward doctor allowlist`. The alias prints a one-line deprecation to stderr and runs the same check. Downstream consumers (notably the cross-repo pre-commit suite) should migrate to `ward doctor allowlist`; the alias removal is tracked as a follow-up.
