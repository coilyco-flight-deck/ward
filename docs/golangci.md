---
doc_goal: Justify ward's golangci-lint posture as deliberate for security-boundary and wire-protocol code, recording why each gosec exclusion (the argv G204, the file-permission and taint variants) and each path-based lint relaxation is safe here, so a contributor trusts the config rather than loosening it blindly.
---
# golangci-lint configuration

`.golangci.yaml` starts from `default: none` and enables a broad set by hand, so
the posture below is deliberate rather than inherited. The correctness core is
errcheck, govet, staticcheck, unused, and ineffassign, with gosec guarding the
security boundary and gocritic, revive (a named rule set), errorlint, bodyclose,
and exhaustive rounding it out. The complexity gates are the sharp edge, because
these packages are security boundaries or wire-protocol layers and tangled
branchy code is where the bugs live: gocyclo and cyclop cap cyclomatic
complexity at 12 (cyclop also holds a package average of 8), gocognit at 20,
funlen at 100 lines / 50 statements, and nestif at a nesting depth of 5. Run
with `ward exec lint`.

## gosec exclusions

- **G204** fires on every `exec.CommandContext(ctx, bin, argv...)` even with
  argv properly constructed. Argv validation happens at the cli-guard policy
  layer; refusing it here would defeat the point of the wrappers.
- **G301/G302/G304/G306** (file permissions) - perms are managed deliberately
  per call site. Trust the per-site choice over a blanket rule.
- **G703** is the taint-analysis variant of G304 (path traversal via a
  variable). ward's file paths are operating-context (env-derived work dirs,
  fixed system locations like `/etc/ward-git-credentials`), not untrusted remote
  input, so the same rationale that excludes G304/G306 applies.

## Lint exclusion rules

- **`_generated\.go$`** - generated files, mostly mechanical, skip most checks.
- **`_test\.go$`** - tests get relaxed complexity; long table-driven cases are
  fine.
- **`^examples/`** - small demonstration mains; structure is illustrative, not
  production code.

## See also

- [exec-verb.md](exec-verb.md) - the `ward exec` gate and the cli-guard argv
  validation layer where the real check behind the G204 exclusion lives, so
  suppressing it here does not leave the `exec.CommandContext` call unchecked.
