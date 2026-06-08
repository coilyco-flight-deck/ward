# golangci-lint configuration

`.golangci.yaml` is strict-ish, leaning on cyclomatic-complexity checks because
these packages are security boundaries or wire-protocol layers and tangled
branchy code is where the bugs live. Run with `coily exec lint`.

## gosec exclusions

- **G204** fires on every `exec.CommandContext(ctx, bin, argv...)` even with
  argv properly constructed. Argv validation happens at the cli-guard policy
  layer; refusing it here would defeat the point of the wrappers.
- **G301/G302/G304/G306** (file permissions) - perms are managed deliberately
  per call site. Trust the per-site choice over a blanket rule.

## Lint exclusion rules

- **`_generated\.go$`** - generated files, mostly mechanical, skip most checks.
- **`_test\.go$`** - tests get relaxed complexity; long table-driven cases are
  fine.
- **`^examples/`** - small demonstration mains; structure is illustrative, not
  production code.
