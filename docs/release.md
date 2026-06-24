# Release pipeline

Forgejo-canonical release on push to `main`
(`forgejo.coilysiren.me/coilyco-flight-deck/ward`). The
`.forgejo/workflows/release.yml` pipeline cuts the tag + release, then bumps the
homebrew formula(e) so `brew upgrade ward` builds the new tag from source.

ward's formula is build-from-source (a per-tag tarball `url` + `sha256` ->
`go build`, since ward#116), so unlike o2r there are no prebuilt binaries to
attach. The `publish-binaries` job still uploads linux binaries as release
assets for convenience, but `brew` never consumes them.

## Version bump

`actions/tag-bump` runs with no bump input, so every push-to-main release is a
minor bump. For a major, cut the `vN.0.0` tag by hand; pushes resume minor from
there.

## Formula bump job

One job rewrites the formula `url` line after a release:

- **bump-tap-formula** - rewrites `Formula/ward.rb` in the centralized
  flight-deck tap (`coilyco-flight-deck/homebrew-tap`), where
  `brew install coilyco-flight-deck/tap/ward` reads from. Runs on the dedicated
  `tap-writer` runner (`infrastructure/deploy/forgejo-runner-tap-writer.yml`): a
  host executor whose system git credential helper supplies the tap-write token
  only when git asks (`action=get`). The token never enters the job env, logs,
  or an Actions secret. The host executor has no node, so every step is pure
  shell - no JS `uses:` actions can run there.

### Failing loudly on write errors

The bump step is `set -euo pipefail` and verifies its own work, so a stalled tap
can never hide behind a green release (ward#237, where v0.97.0-v0.102.0 shipped
without bumping the tap because the tap-write credential broke):

- `pipefail` aborts if the tarball fetch behind the piped `sha256` fails, instead
  of hashing an empty body into a bogus digest.
- The computed `sha256` must be a 64-hex digest before any formula is written.
- A non-zero `git push` (the symptom of a missing or rotated tap-write
  credential) fails the job with an `::error::` annotation naming the likely
  cause.
- After pushing, the step re-reads the tap's `main` and asserts it now serves the
  new tag; a push that reports success but does not land fails the release.

If a release goes red here, the fix is operational: restore the tap-writer
runner's git credential (the `infrastructure` deploy that provisions it). The
bump is idempotent and backfilling - it rewrites `url`/`sha256` against whatever
the live tap currently holds - so the next green release advances the tap to the
latest tag regardless of how many bumps were missed.

The prior in-repo `bump-formula` fallback (which rewrote ward's own
`Formula/ward.rb` via the Contents API on the `docker` runner) was removed: it
duplicated the tap bump, was already marked deprecated, and failed every release
because that runner has no `jq`. The in-repo `Formula/ward.rb` itself has since
been deleted - the tap is the single source `brew` installs from. See
[ci-watch.md](ci-watch.md) for `ward ci watch`, the verb that surfaced this.

The bump carries the `[skip ci]` marker so the formula commit does not
re-trigger the workflow. Shared composite actions live at
`coilysiren/agentic-os/actions/*`. This replaced the prior `.github/workflows`
release; building moved off GitHub Actions onto Forgejo.
