# The `issue create --quiet` machine-output mode (ward#316)

`ward ops forgejo issue create` renders the whole created-issue object through
the `specverb` engine's default **YAML**. The one field a programmatic caller
needs - `number:` - is buried in that block, bare and unquoted, with no terse,
parse-stable alternative. Filing ward#313 a caller grepped the success output for
`"number"|html_url|error`, the unquoted `number:` did not match, a successful
create read as a failure, and a duplicate issue (ward#314) was filed.

The engine has no per-call output-shaping hook and cli-guard is a pinned
upstream, so ward owns the success line for this one leaf. After
`specverb.Build`, `overrideForgejoCreateIssue` (in `cmd/ward/ops.go`) grafts a
`--quiet` flag onto the built `issue create` leaf and wraps its action:

- **without `--quiet`** (and under `--dry-run`) the engine action runs
  untouched - the YAML default and every internal `--output json` caller
  (`forgejo_ops.go`'s `createIssue`) are unchanged;
- **with `--quiet`** the wrapper forces the engine to project just the new
  number (`--output text --query number`), captures that one scalar, and prints
  the terse `{owner}/{repo}#N` ref. A non-numeric capture is rejected as an
  error rather than passed through as a bogus ref;
- failure is left to the **exit code** - on error nothing lands on stdout, so a
  caller distinguishes success from failure without parsing output at all.

`--quiet` refuses to combine with `--output`/`--query` (it owns both), and the
full JSON object stays available the engine-standard way, `--output json`.

This is the same trade as the lean `issue view` override
([ops-forgejo-view.md](ops-forgejo-view.md)): the engine can't shape the bytes,
so ward does. A per-call projection (or a programmatic-capture API) in cli-guard
would let this fold back into the guardfile - a follow-up.

## See also

- [ops-forgejo-view.md](ops-forgejo-view.md) - the sibling lean `issue view` override.
- [ops-forgejo-in-ward.md](ops-forgejo-in-ward.md) - the in-binary mount + read seams.
