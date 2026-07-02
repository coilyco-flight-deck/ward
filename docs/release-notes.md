# Release notes

The release body is not the raw `git log` dump `tag-bump` emits. The `notes`
step in the `release` job (`.forgejo/workflows/release.yml`) pipes the same
commit range (`previous_tag..HEAD`) through
[`scripts/release-notes.sh`](../scripts/release-notes.sh), which categorises it
into a template a downstream self-hoster can actually read (ward#486).

## Why

Every release body used to be a flat `git log` dump of conventional-commit
subjects: internal refactor jargon, bare short SHAs, bare `ward#NNN` refs, with
nothing to tell "internal refactor, safe to ignore" apart from "behavior
changed, read before upgrading". The `forgejo-selfhoster` persona - the one
audience that consumes the Linux binaries directly - had to read raw commit
history and infer intent to answer "does this upgrade matter to me".

## The template

- A one-line **"Does this upgrade affect you?"** verdict up top: **Yes** when the
  range carries a breaking/behavior change, **Maybe** for features/fixes, and
  **Probably not** for internal-only churn.
- **Breaking / behavior changes** first, each flagged by a `!` in the
  conventional-commit header (`feat!:`, `fix(x)!:`) or a `BREAKING CHANGE:` note
  in the commit body - the "note in the commit body" AGENTS.md's API-break policy
  calls for, now surfaced instead of buried.
- **Features** and **Fixes** as their own sections, **Performance** when present.
- Routine internal churn (refactors, docs, chores, tests) folded under a
  collapsed `<details>` so it stays present but out of the way.
- A **Full changelog** compare link back to the raw range.

## Shape and testing

The script reads NUL-separated `%h\t%s\t%b` git-log records on stdin, so it is a
pure text transform driven by fixtures in
[`scripts/release_notes_test.go`](../scripts/release_notes_test.go) (run under
`ward exec test`), with no live git state.

Keeping the categoriser in bash also keeps the critical tag/release-creation job
dependency-free (bash + awk + git, no Go toolchain fetch) - the same reliability
discipline as the `tag-bump` and `create-release` composite actions. If the
script errors or yields an empty body, the `notes` step logs a `::warning::` and
falls back to `tag-bump`'s raw changelog, so a release never ships an empty body.

## See also

- [release.md](release.md) - the full release pipeline.
- [scripts/release-notes.sh](../scripts/release-notes.sh) - the categoriser.
