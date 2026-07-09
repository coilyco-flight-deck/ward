# setup/doctor release plan

This is the pre-release inventory for the retired `ward setup` and `ward doctor`
surfaces.

The current command pair is being removed from the release surface now. A smaller
replacement will be rebirthed immediately before release.

## `ward setup` behavior inventory

- Starts from `--dir` or the current working directory.
- Walks upward to the first ancestor that contains a `Makefile`.
- Parses every Makefile target with a `## <description>` help comment.
- Keeps the first-seen description for duplicate target names.
- Filters target names to lowercase letters, digits, and interior dashes.
- Reports invalid target names as skipped and omits them from the scaffold.
- Fails when no documented targets are found.
- Writes `.ward/ward.yaml` at the repo root under `.ward/`.
- Refuses to overwrite an existing config unless `--force` is set.
- Renders each accepted target as `run: make <name>` plus the copied description.
- YAML-quotes descriptions only when needed to preserve the contract.
- Appends a commented `security:` scaffold that is inert until uncommented.
- Prints the written path, verb count, and any skipped Makefile targets.
- Prints a prune note telling the user to remove unwanted verbs and tailor security.
- Runs `ward doctor` against the generated file unless `--skip-doctor` is set.
- Downgrades the missing-`security:` result to a note for the generated scaffold.
- Treats `warded setup` as a carve-out from the `warded -> ward agent` rewrite.

## `ward doctor` behavior inventory

- Resolves config by explicit `--config`, then `WARD_CONFIG`, then walk-up from cwd.
- Validates the resolved `.ward/ward.yaml` against the repo `Makefile`.
- Emits one allowlist OK line when the config matches.
- Emits line-anchored allowlist problems when it does not.
- Appends the allowlist contract hint about `run: make <name>` and `## <description>`.
- Loads the parsed `security:` block and prints a summary line.
- Fails when no `security:` block is declared unless the caller allows the missing block.
- Probes each protected binary path with `exec.LookPath`.
- Probes passwordless sudo with `sudo -n true` when `forbid_passwordless` is set.
- Probes each `credential_env` entry and reports the env vars that are set.
- Promotes credential hits from `WARN` to `FAIL` when `--strict-credentials` is set.
- Probes local-harness Ollama reachability from `WARD_OLLAMA_URL` and `WARD_GOOSE_OLLAMA_HOST_B64`.
- Skips the Ollama probe when neither env is configured, or when `--skip ollama` is set.
- Emits one row per probe result and returns non-zero on any `FAIL`.

