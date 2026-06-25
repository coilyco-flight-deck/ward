# The lean `issue view` override (ward#225)

`ward ops forgejo issue view` combines the issue and its comment thread, but the
`specverb` engine renders each call's response **verbatim** - and the Forgejo
API nests every commenter's full profile (bio, avatar, counts, ...) into every
comment. A 5-comment issue therefore printed the same profile five times when
the reader only wants the username (`coilysiren`'s report).

The engine has no per-call projection hook and cli-guard is a pinned upstream,
so ward owns the rendering for this one leaf. After `specverb.Build`,
`overrideForgejoViewIssue` (in `cmd/ward/ops.go`) swaps the built `issue view`
leaf's action for `runForgejoViewIssue`, which fetches the issue + comments
through `forgejo_issue.go`'s read seam (`viewIssue`) and prints a lean
`{issue, comments}` shape with every user collapsed to its login literal.

The override:

- reproduces the guardfile's `restrict owner matches coily*` scope gate the
  engine leaf enforced, and honors the leaf's `--output` / `--dry-run` flags;
- leaves the guardfile shape untouched - `move-issue`'s internal `call view
  issue` resolves the `can view issue` grant (the raw leaf), not this CLI leaf,
  so its title/body data-flow is unaffected.

This is the same trade as the scalar read seam in
[ops-forgejo-in-ward.md](ops-forgejo-in-ward.md): the engine can't shape the
bytes, so ward does. A per-call projection in cli-guard would let this fold back
into the guardfile - a follow-up, like the programmatic-capture API.

## See also

- [ops-forgejo-quiet.md](ops-forgejo-quiet.md) - the sibling `issue create --quiet` machine-output override.
- [ops-forgejo-in-ward.md](ops-forgejo-in-ward.md) - the in-binary mount + read seams.
- [ops-forgejo.md](ops-forgejo.md) - the ward-kdl proving ground + guardfile.
