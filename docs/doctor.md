---
doc_goal: Make ward doctor readable as the loud validator that turns a .ward/ward.yaml into a trustworthy policy gate - the allowlist drift guard (stricter than target-name-exists), the fail-when-no-security-block behavior that makes exit 0 mean a policy is in force, the three host security probes, and the host-side Ollama-reachability probe that mirrors the local-harness launch gate before dispatch - so a reader can wire it as a CI gate and read every row.
---
# ward doctor

`ward doctor` is ward's single diagnostic verb. Runs every check inline and exits non-zero on any failure. Reads the resolved config path (`--config` > `$WARD_CONFIG` > walk-up from cwd, per [config-discovery](config-discovery.md)).

## Checks

- **Allowlist.** Validates the resolved `.ward/ward.yaml` (or `.coily/coily.yaml`) against the repo's `Makefile`. Engine lives upstream in `cli-guard/allowlist`; ward only supplies the resolved paths and renders the returned `Problem` set. See [Allowlist contract](#allowlist-contract) for what makes a target "match" - it is stricter than target-name-exists.
- **Security: summary.** Reports the parsed `security:` block. A repo with **no** `security:` block **fails** doctor (`no security: declared`, non-zero exit), so exit 0 means a policy is declared and in force, not merely that nothing was misconfigured. Without the block the dev-verb gate (clean-tree, argv, audit) still runs, but no protected-binary / sudo / hook policy is enforced. `ward setup` reports the absence as a `NOTE`, not a failure. This makes `ward doctor` a safe CI policy gate.
- **Security: host probes.** Three probes against the parsed block. `FAIL` rows drive the exit code; `WARN`, `INFO`, `PASS`, and `SKIP` only surface text.
  - **`path`.** Resolves each `protected_binaries[].name` via `exec.LookPath`. When `expected_real_paths` is non-empty, a mismatch is a `FAIL`. When the list is empty, the resolved location surfaces as `INFO`. A missing binary is a `WARN`.
  - **`sudo`.** Skipped unless `sudo.forbid_passwordless` is set. Runs `sudo -n true`. Clean exit is `FAIL`; non-zero with a "password required" sentinel is `PASS`; any other non-zero is `WARN`.
  - **`credentials`.** Walks every `protected_binaries[].credential_env` name and reports which are set in this session. Each hit is a `WARN` by default. `--strict-credentials` promotes hits to `FAIL`.
- **Ollama reachability.** A host-side mirror of the local-harness launch gate ([ward#487](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/487)), so a down endpoint surfaces before a `ward agent` goose/opencode dispatch would hang on it ([ward#499](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/499)). It reads the same env the dispatch path binds - `WARD_OLLAMA_URL` (opencode) and `WARD_GOOSE_OLLAMA_HOST_B64` (goose, base64, resolved from the SSM tower host) - TCP-dials each once, and emits a row per endpoint: reachable is `PASS`, unreachable is `FAIL` (naming the endpoint and the `WARD_SMOKE_TEST_SKIP=1` bypass). With **neither** env set it emits a single `SKIP` - the baked fleet default is not operator intent, so a plain adopter run (no local harness in play) stays green and the in-container launch gate stays the fallback. It is a **reachability** check, not a model-serving one, exactly like the launch gate. `--skip ollama` stands it down; it is independent of the `security:` block. See [agent-local-harnesses.md](agent-local-harnesses.md).

## Allowlist contract

The allowlist check is a **drift guard**, not a target-name lookup: it keeps the verb surface (`.ward/ward.yaml`) and the make-target surface (`Makefile`) from silently diverging. For each `commands.<name>` entry, all three must hold or the check reports a `Problem`:

- **`run:` is exactly `make <name>`.** `run: bash build.sh` under a `build:` key is a mismatch, even if the script works.
- **The Makefile has a `<name>:` rule carrying a `## <description>` help comment.** This is the self-documenting-Makefile convention (`build: ## Build all packages.`). A bare `build:` rule with only a recipe and **no** `## ...` comment is **not registered as a target** - so the check reports `commands.build has no matching Makefile target` even though `make build` runs correctly by hand.
- **That `## <description>` text equals the command's `description:`** in the yaml, trimmed. Any wording difference is reported as a description drift with both sides quoted.

So the minimal Makefile that satisfies the check for a `build`/`test`/`install` triple is:

```makefile
build: ## Build all packages.
	@echo building
test: ## Run the unit test suite.
	@echo testing
install: ## Install.
	@echo installing
```

paired with:

```yaml
commands:
  build: {run: make build, description: Build all packages.}
  test: {run: make test, description: Run the unit test suite.}
  install: {run: make install, description: Install.}
```

When the check fails, ward appends a one-line hint naming this contract. Only targets you expose through `ward.yaml` need the `## ` comment - internal helper targets can stay bare and are simply ignored.

## Flags

- `--skip <name>` - repeatable. Suppresses a security probe (`path`, `sudo`, or `credentials`) and surfaces a `SKIP` row in its place.
- `--strict-credentials` - promotes credential-env hits from `WARN` to `FAIL` for CI use.

ward parses but does not enforce the `security:` block beyond these probes. PreToolUse hook wiring for protected-binary denial lives in `ward hook pre-tool-use` (see [hook.md](hook.md)).
