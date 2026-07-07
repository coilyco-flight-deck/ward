---
doc_goal: Let a maintainer trust and repair ward's Forgejo-canonical push-to-main release path - the minor-bump tag, the dual-forge binary matrix, and the loud fail-or-verify tap-formula bump - understanding why each guard exists and exactly which Actions secret to rotate when a release goes red.
---
# Release pipeline

Forgejo-canonical release on push to `main`. The
`.forgejo/workflows/release.yml` pipeline cuts the tag + release, then bumps the
homebrew formula(e) so `brew upgrade ward` builds the new tag from source.

ward's formula is build-from-source (a per-tag tarball `url` + `sha256` ->
`go build`), but `publish-binaries` still ships the full matrix + `SHA256SUMS`
to **both** the Forgejo and GitHub release pages ([release-binaries.md](release-binaries.md)).

## The aos ward-specs bundle overlay (the coilyco surface source)

Before the cross-compile loop, `publish-binaries` overlays the coilyco deployment
surface from an aos-published release asset rather than compiling it straight from
ward's own tree - [ward#503](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/503) step 3, the residual of the ward-kdl
spec-bundle cutover. aos publishes the values (forgejo guardfile + swagger lock,
the fleet manifest, the spec locks) as a pinned, checksummed
`ward-specs-<tag>.tar.gz` release asset
([aos#315](https://github.com/coilysiren/agentic-os/issues/315), the aos-side
half). The **Overlay** step:

- **Pins the aos tag explicitly** (`AOS_SPECS_TAG` in the `publish-binaries`
  `env:`) and **fails closed on a blank pin**, mirroring the `-z` secret guards
  the other jobs use - a released binary never builds from an unpinned bundle.
- **Fetches + sha256-verifies** the asset against its `.sha256` sidecar (a bad
  digest or a mismatch fails the release, never overlays garbage), extracts it
  onto `.ward/ward-kdl/` (the assets-dir convention, [ward#453](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/453)), and
  re-derives the three shipped embed dirs (`opsassets`, `fleetassets`,
  `execassets`) by the same file-copy `make sync-*-assets` uses - no live spec
  re-fetch, no generator.
- **Proves the no-op two ways.** It asserts (fail-closed) that the overlaid
  ops-forgejo guardfile still carries the coilyco `base-url` + `owner` coupling, so
  a wrong / neutral / foreign bundle can never ship. Then, because the overlay is
  a byte no-op only when aos and ward carry identical values, it **fails safe**:
  if the aos bundle lags ward's tree (a value that landed in ward but is not yet
  homed in aos), it restores ward's committed embeds and warns loudly rather than
  ship a regression or fail every release red. So the release stays byte-identical
  to today's shipped coilyco surface until aos catches up.

This is **step 3** of the staged, no-op-then-flip cutover. Two steps remain and
are **not** done here: **step 2** (the homebrew-tap `Formula/ward.rb` overlay,
Kai's GitHub-side human gate) and **step 4** (neutralizing ward's own tracked
`.ward/ward-kdl/` tree). Step 4 is safe only once **both** build sites overlay,
and it must **remove the fail-safe revert** above - once the tree is neutral the
overlay is supposed to change the embeds. See
[ward-kdl-authoring.md](ward-kdl-authoring.md).

The release page carries **only** the `ward` binaries (+ checksums): `ward-kdl`
and its `ward-kdl-{read,write,admin}` tiers are no longer public assets - embedded
in `ward`, spec authors build from a clone ([authoring](ward-kdl-authoring.md)). Two
tiers are the exception, each pushed to an **internal** generic package registry
keyed to the release tag, never the public release page
([ward#501](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/501)):

- **`publish-kdl-write`** - the write tier the in-container broker shells
  ([broker.md](broker.md)).
- **`publish-kdl-read`** - the read tier a **sealed read-only director** session
  pulls via the entrypoint's `install_ward_kdl_read`, the non-mutating
  ssh-through-docker observe surface
  ([ward#547](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/547),
  [ward#572](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/572)).
  Without this producer the entrypoint's best-effort fetch 404'd every run, so a
  director session never got the helper.

Both jobs mirror each other exactly and share the same `CI_RELEASE_TOKEN`
package-write requirement below.

## Version bump

`actions/tag-bump` runs with no bump input, so every push-to-main release is a
minor bump. For a major, cut the `vN.0.0` tag by hand; pushes resume minor from
there. Release bodies are categorised
([release-notes.md](release-notes.md)).

## Packaging-manifest bump jobs

Two sibling jobs write ward's package-manager manifests at release time, one per
OS, so every install channel is a **push** from the tag build rather than a poll:

- **bump-tap-formula** - rewrites `Formula/ward.rb` in the centralized
  flight-deck tap (`coilyco-flight-deck/homebrew-tap`), where
  `brew install coilyco-flight-deck/tap/ward` reads from. Runs on the `docker`
  runner and authenticates the push with the `TAP_WRITE_TOKEN` repo Actions
  secret carried in the push URL (never echoed; git masks credentials in any URL
  it prints), mirroring how `publish-binaries` uses `CI_RELEASE_TOKEN`. The job
  guards up front and fails loudly if the secret is unset.
- **bump-scoop-manifest** - the Windows sibling ([ward#571](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/571)). Writes the whole
  `bucket/ward.json` (version + the amd64/arm64 release URLs + the two windows
  hashes) into the scoop bucket (`coilyco-flight-deck/scoop-bucket`) and pushes,
  authenticating with `SCOOP_WRITE_TOKEN` exactly as the tap job uses
  `TAP_WRITE_TOKEN`. Before this job the manifest refreshed only on a bucket-side
  daily autoupdate poll, so it lagged however many releases landed between runs
  (a minor bump every push to main leaves a busy day several versions behind).
  Now the tag build writes it, matching every other artifact.
  - **It targets the Forgejo bucket, not the GitHub mirror.** The bucket is
    Forgejo-canonical: users add it with
    `scoop bucket add ... https://forgejo.coilysiren.me/...` and its `checkver`
    reads the Forgejo `releases.atom` feed, so `scoop update ward` only sees a
    manifest that is current on **Forgejo**. Pushing to the mirror alone would
    not clear the user-facing lag, so the job writes where scoop reads - the
    homebrew-tap sibling is Forgejo for the same reason.
  - The manifest hashes come from the per-asset `.exe.sha256` sidecars
    `publish-binaries` uploads (the scoop autoupdate contract, [ward#561](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/561)), so the
    job `needs: [release, publish-binaries]`.
  - It writes the **full** manifest (not an in-place version patch), so it is
    self-healing: the first release creates `ward.json` even though the bucket
    has none today, and ward stays the single source of truth for its own
    manifest. The retained `checkver`/`autoupdate` blocks keep a bucket-side poll
    working as a safety net if a push is ever missed.

#### Required Actions secrets

Set both in ward -> Settings -> Actions -> Secrets:

- `CI_RELEASE_TOKEN` - `publish-binaries` uploads release assets with it, and
  `publish-kdl-write` + `publish-kdl-read` push the write and read tiers (so it
  needs **package write** scope too).
- `TAP_WRITE_TOKEN` - used by `bump-tap-formula` to push the formula bump.
  Scope: push to `coilyco-flight-deck/homebrew-tap`.
- `SCOOP_WRITE_TOKEN` - used by `bump-scoop-manifest` to push the scoop
  manifest bump. Scope: push to `coilyco-flight-deck/scoop-bucket`. Unset, the bump
  job fails loudly (a red job on an otherwise-green release, the same
  fail-not-freeze stance as the tap) so the manifest cannot silently lag again;
  provision it once, like `TAP_WRITE_TOKEN`.

##### `publish-kdl-write` / `publish-kdl-read` red? Two distinct faults ([ward#567](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/567))

Both tier producers PUT to the same generic registry and share this diagnostic.
The generic-package PUT has two independent ways to fail, and the job's diagnostic
names which one it hit:

- **HTTP 401 `reqPackageAccess` (operational)** - `CI_RELEASE_TOKEN` authenticates
  for the release-asset API but lacks **package (`write:package`) scope** for the
  `coilyco-flight-deck` registry. Release assets keep shipping, but the write tier
  never lands. Fix: add package-write scope to the secret (or rotate it for one that
  carries it), then re-release - the bump is not backfilling, so a later tag simply
  publishes going forward. The built-in Forgejo Actions token is **not** a
  substitute: this instance denies it package access (verified) even with
  `permissions: packages: write` declared.
- **HTTP 500 `request Content-Type isn't multipart/form-data` (code, fixed)** - the
  generic uploader **requires** an explicit `Content-Type: application/octet-stream`
  header. Without it curl's `--data-binary` defaults to
  `application/x-www-form-urlencoded` and Forgejo 500s. The release-asset upload
  always set this header; the generic PUT now does too. This is the latent second
  wall behind the 401 - it surfaces only once the token scope is fixed.

The prior `tap-writer` runner credential helper
(`infrastructure/deploy/forgejo-runner-tap-writer.yml`) is retired: it broke
silently and froze the tap at v0.96.0. The
credential now lives in one rotatable, visible place instead of a hidden runner
config.

### Failing loudly on write errors

The bump step is `set -euo pipefail` and verifies its own work, so a stalled tap
can never hide behind a green release (where v0.97.0-v0.102.0 shipped
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
