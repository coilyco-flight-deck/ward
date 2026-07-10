---
doc_goal: Let a maintainer trust and repair ward's Forgejo-canonical push-to-main release path - the minor-bump tag, the dual-forge binary matrix, and the loud fail-or-verify tap-formula bump - understanding why each guard exists and exactly which Actions secret to rotate when a release goes red.
---
# Release pipeline

Forgejo-canonical release on push to `main`. The
`.forgejo/workflows/release.yml` pipeline cuts the tag + release, then bumps the
homebrew formula(e) so the package manager downloads and verifies the tagged
release binary.

ward's formula downloads the per-platform release binaries (`url` + `sha256`),
but `publish-binaries` still ships the full matrix + `SHA256SUMS` to **both**
the Forgejo and GitHub release pages ([release-binaries.md](release-binaries.md)).

## No build-time config overlay (superseded by live resolve)

`publish-binaries` compiles straight from ward's tree: the released binary
embeds the tracked `.ward/ward-kdl/` mirrors as the **neutral baked default**, and
operator config is resolved **live at launch** through the `WARD_CONFIG_REF`
config-source seam ([config-source.md](config-source.md), [ward#653](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/653), epic
[ward#650](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/650)). The former aos `ward-specs` bundle overlay - fetched,
sha256-verified, and copied over the embeds before the cross-compile loop
([ward#644](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/644), [ward#503](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/503) step 3) - is removed along with the build-variant
matrix it implied: one prebuilt binary per platform, byte-identical across
forges, no per-deployment builds. The tap and the scoop manifest now download
and verify those same per-platform release binaries.

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
  ssh-through-docker helper ([ward#572](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/572)).
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
  refreshes the per-platform release-asset URLs and checksums, and guards up
  front so a missing secret fails loudly. It depends on `publish-binaries`, so
  the formula hashes are computed only after the binary assets are uploaded; a
  missing asset is reported with the exact release URL that failed.
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
    reads the Forgejo `releases.atom` feed, so scoop only sees a manifest that
    is current on **Forgejo**. Pushing to the mirror alone would
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

- `pipefail` aborts if a release-asset fetch behind the piped `sha256` fails,
  instead of hashing an empty body into a bogus digest.
- Every computed `sha256` must be a 64-hex digest before any formula is written.
- The step fails up front with an `::error::` annotation if `TAP_WRITE_TOKEN` is
  unset.
- A non-zero `git push` is retried up to three times before the job fails with
  an `::error::` annotation naming the likely cause. That covers short-lived
  transport blips without hiding real secret-scope failures.
- If the tap already serves the same tag, or has already moved past it to a
  newer version, the job exits successfully instead of trying to downgrade the
  tap with a stale release bump.
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
