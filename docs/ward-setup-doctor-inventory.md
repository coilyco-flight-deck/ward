# ward setup and doctor inventory

This is the release-planning inventory for the current `setup` and `doctor`
surfaces. The live commands are being removed from the release surface now, and
a smaller replacement will be rebirthed immediately before release.

## `ward setup`

Current behavior inventory:

- Finds the repo root by walking upward from `--dir` or the current directory
  until it hits a `Makefile`.
- Parses documented Makefile targets in source order.
- Treats only `target: ... ## description` lines as candidates.
- Accepts a target only when the name matches ward verb rules:
  lowercase letters, digits, and interior dashes only.
- Keeps the first description when a target appears more than once.
- Reports invalid target names as skipped.
- Refuses to write `.ward/ward.yaml` when it already exists unless `--force`
  is set.
- Creates `.ward/` as needed and writes the scaffolded config with mode 0644.
- Emits a summary line naming the output path, the verb count, and the Makefile
  path.
- Emits one skipped-target line per invalid verb name.
- Emits a prune-and-tailor reminder after writing.
- Optionally skips the follow-up doctor pass with `--skip-doctor`.
- Otherwise runs the generated config through doctor with the missing-security
  state downgraded to a note.

Current scaffold contents:

- One `commands:` entry per valid documented Makefile target.
- A commented `security:` template.
- The template is inert until uncommented.

## `ward doctor`

Current behavior inventory:

- Resolves the config path with this precedence:
  `--config` > `$WARD_CONFIG` > walk-up from cwd.
- Runs the allowlist lint against the resolved config and the repo Makefile.
- Summarizes the parsed `security:` block before any probe output.
- Fails when no `security:` block is declared, unless the setup path has
  explicitly allowed the missing block.
- Runs the PATH posture probe for each protected binary.
- Runs the passwordless-sudo probe when `forbid_passwordless` is set.
- Runs the credential-env probe for every configured credential variable.
- Runs the host-side Ollama reachability probe unless skipped.
- Accepts repeatable `--skip` values for `path`, `sudo`, `credentials`, and
  `ollama`.
- Accepts `--strict-credentials` to promote credential hits from WARN to FAIL.
- Prints one rendered row per probe result.
- Returns a joined failure string when any row fails.

Output paths:

- Allowlist success prints an OK line naming the config path.
- Allowlist failure prints file and line anchored problems, then a contract
  hint.
- Security success prints a one-line summary.
- Missing security prints the summary plus a note or a failure hint, depending
  on the caller.
- Probe rows print `probe`, severity, and detail in a fixed-width format.
- Ollama failures join into the overall error string.

