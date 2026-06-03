# ward doctor

`ward doctor` is ward's diagnostic umbrella. It is the single verb a contributor or CI step calls to validate that the repo's `.ward/ward.yaml` and host posture are in shape. Every check group reads the resolved config path (`--config` > `$WARD_CONFIG` > walk-up from cwd, per [config-discovery](config-discovery.md)).

## Invocations

- `ward doctor` — runs every check group. Exits non-zero on any failure; partial summaries still flush so an operator can see what passed.
- `ward doctor allowlist` — validates `.ward/ward.yaml` (or `.coily/coily.yaml`) against the repo's `Makefile`. Same contract `ward lint` enforced; the alias still works for one minor release.
- `ward doctor security` — summarizes the parsed `security:` block from cli-guard's `repocfg`. Reports protected-binary count, sudo posture, and hook-policy presence. A config with no `security:` block is a pass and reports `no security: declared`.

## Probes

`ward doctor security` runs three host probes against the parsed `security:` block. Each probe emits one or more rows tagged with a severity. `FAIL` rows drive the exit code; `WARN`, `INFO`, `PASS`, and `SKIP` only surface text.

- **`path` — PATH posture per protected binary.** Resolves each `protected_binaries[].name` via `exec.LookPath`. When `expected_real_paths` is non-empty, a mismatch is a `FAIL`. When the list is empty, the resolved location surfaces as `INFO`. A missing binary is a `WARN` (it may simply not be installed on this host).
- **`sudo` — passwordless sudo.** Skipped unless `sudo.forbid_passwordless` is set. Runs `sudo -n true` non-interactively. A clean exit is a `FAIL` (passwordless sudo is reachable from this session). A non-zero exit with a "password required" sentinel is a `PASS`. Any other non-zero exit is a `WARN`.
- **`credentials` — credential env scan.** Walks every `protected_binaries[].credential_env` name and reports which are set in this session. Each hit is a `WARN` by default. `--strict-credentials` promotes hits to `FAIL` so a CI step can refuse to run with credentials on the bus.

`--skip <name>` (repeatable) suppresses a probe and surfaces a `SKIP` row in its place. Useful for hosts that lack the dependency (no `sudo` binary, etc).

ward parses but does not enforce the `security:` block beyond these probes. PreToolUse hook wiring for protected-binary denial is tracked as a separate sub-issue of #4.

## Output

Each check group prints one summary line to stdout on success. On failure, the group's error is prefixed with the group name (`allowlist: …`, `security: …`) and surfaces on stderr via cli's standard error path. `ward doctor` aggregates failures across groups before returning, so the exit code reflects any failure even when later groups passed.

## Migration from `ward lint`

`ward lint` is a thin alias for `ward doctor allowlist`. The alias prints a one-line deprecation to stderr and runs the same check. Downstream consumers (notably the cross-repo pre-commit suite) should migrate to `ward doctor allowlist`; the alias removal is tracked as a follow-up.
