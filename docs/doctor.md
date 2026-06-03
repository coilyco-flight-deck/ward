# ward doctor

`ward doctor` is ward's single diagnostic verb. Runs every check inline and exits non-zero on any failure. Reads the resolved config path (`--config` > `$WARD_CONFIG` > walk-up from cwd, per [config-discovery](config-discovery.md)).

## Checks

- **Allowlist.** Validates the resolved `.ward/ward.yaml` (or `.coily/coily.yaml`) against the repo's `Makefile`. Engine lives upstream in `cli-guard/allowlist`; ward only supplies the resolved paths and renders the returned `Problem` set.
- **Security: summary.** Reports the parsed `security:` block — protected-binary count, sudo posture, hook-policy presence. A config with no `security:` block is a pass and reports `no security: declared`.
- **Security: host probes.** Three probes against the parsed block. `FAIL` rows drive the exit code; `WARN`, `INFO`, `PASS`, and `SKIP` only surface text.
  - **`path`.** Resolves each `protected_binaries[].name` via `exec.LookPath`. When `expected_real_paths` is non-empty, a mismatch is a `FAIL`. When the list is empty, the resolved location surfaces as `INFO`. A missing binary is a `WARN`.
  - **`sudo`.** Skipped unless `sudo.forbid_passwordless` is set. Runs `sudo -n true`. Clean exit is `FAIL`; non-zero with a "password required" sentinel is `PASS`; any other non-zero is `WARN`.
  - **`credentials`.** Walks every `protected_binaries[].credential_env` name and reports which are set in this session. Each hit is a `WARN` by default. `--strict-credentials` promotes hits to `FAIL`.

## Flags

- `--skip <name>` — repeatable. Suppresses a security probe (`path`, `sudo`, or `credentials`) and surfaces a `SKIP` row in its place.
- `--strict-credentials` — promotes credential-env hits from `WARN` to `FAIL` for CI use.

ward parses but does not enforce the `security:` block beyond these probes. PreToolUse hook wiring for protected-binary denial lives in `ward hook pre-tool-use` (see [hook.md](hook.md)).
