# Release pipeline

Forgejo-canonical release on push to `main`
(`forgejo.coilysiren.me/coilyco-flight-deck/ward`). The
`.forgejo/workflows/release.yml` pipeline cuts the tag + release, then bumps the
homebrew formula(e) so `brew upgrade ward` builds the new tag from source.

ward's formula is build-from-source (a per-tag tarball `url` + `sha256` ->
`go build`, since ward#116), so `brew` never consumes them - but the
`publish-binaries` job still ships the full matrix + `SHA256SUMS` to **both** the
Forgejo and GitHub release pages (ward#454, [release-binaries.md](release-binaries.md)).

The release page carries **only** the `ward` binaries (+ checksums): `ward-kdl`
and its `ward-kdl-{read,write,admin}` tiers are no longer public assets (ward#455)
- embedded in `ward`, spec authors build from a clone ([authoring](ward-kdl-authoring.md)).
The broker's write-tier download 404s (ward#441); follow-up ward#501.

## Version bump

`actions/tag-bump` runs with no bump input, so every push-to-main release is a
minor bump. For a major, cut the `vN.0.0` tag by hand; pushes resume minor from
there. Release bodies are categorised
([release-notes.md](release-notes.md), ward#486).

## Formula bump job

One job rewrites the formula `url` line after a release:

- **bump-tap-formula** - rewrites `Formula/ward.rb` in the centralized
  flight-deck tap (`coilyco-flight-deck/homebrew-tap`), where
  `brew install coilyco-flight-deck/tap/ward` reads from. Runs on the `docker`
  runner and authenticates the push with the `TAP_WRITE_TOKEN` repo Actions
  secret carried in the push URL (never echoed; git masks credentials in any URL
  it prints), mirroring how `publish-binaries` uses `CI_RELEASE_TOKEN`. The job
  guards up front and fails loudly if the secret is unset.

#### Required Actions secrets

Set both in ward -> Settings -> Actions -> Secrets:

- `CI_RELEASE_TOKEN` - used by `publish-binaries` to upload release assets to
  ward's own releases.
- `TAP_WRITE_TOKEN` - used by `bump-tap-formula` to push the formula bump.
  Scope: push to `coilyco-flight-deck/homebrew-tap`.

The prior `tap-writer` runner credential helper
(`infrastructure/deploy/forgejo-runner-tap-writer.yml`) is retired: it broke
silently and froze the tap at v0.96.0 (the root cause behind ward#237). The
credential now lives in one rotatable, visible place instead of a hidden runner
config.

### Failing loudly on write errors

The bump step is `set -euo pipefail` and verifies its own work, so a stalled tap
can never hide behind a green release (ward#237, where v0.97.0-v0.102.0 shipped
without bumping the tap because the tap-write credential broke):

- `pipefail` aborts if the tarball fetch behind the piped `sha256` fails, instead
  of hashing an empty body into a bogus digest.
- The computed `sha256` must be a 64-hex digest before any formula is written.
- The step fails up front with an `::error::` annotation if `TAP_WRITE_TOKEN` is
  unset.
- A non-zero `git push` (the symptom of a missing, rotated, or under-scoped
  `TAP_WRITE_TOKEN`) fails the job with an `::error::` annotation naming the
  likely cause.
- After pushing, the step re-reads the tap's `main` and asserts it now serves the
  new tag; a push that reports success but does not land fails the release.

If a release goes red here, the fix is operational: set or rotate the
`TAP_WRITE_TOKEN` Actions secret. The bump is idempotent and backfilling, so the
next green release advances the tap to the latest tag regardless of how many
bumps were missed.

The prior in-repo `bump-formula` fallback was removed (it duplicated the tap bump
and failed every release - the `docker` runner has no `jq`), and ward's own
`Formula/ward.rb` deleted: the tap is the single source `brew` installs from.

The bump carries the `[skip ci]` marker so the formula commit does not re-trigger
the workflow. Shared composite actions live at `coilysiren/agentic-os/actions/*`;
building moved off GitHub Actions onto Forgejo.
