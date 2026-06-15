# Release pipeline

Forgejo-canonical release on push to `main`
(`forgejo.coilysiren.me/coilyco-flight-deck/ward`). The
`.forgejo/workflows/release.yml` pipeline cuts the tag + release, then bumps the
homebrew formula(e) so `brew upgrade ward` builds the new tag from source.

ward's formula is build-from-source (url+tag+revision -> `go build`), so unlike
o2r there are no prebuilt binaries to attach.

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

The prior in-repo `bump-formula` fallback (which rewrote ward's own
`Formula/ward.rb` via the Contents API on the `docker` runner) was removed: it
duplicated the tap bump, was already marked deprecated, and failed every release
because that runner has no `jq`. The tap is the single source `brew` installs
from, so the in-repo copy bought nothing. See [ci-watch.md](ci-watch.md) for the
helper that surfaced this.

The bump carries the `[skip ci]` marker so the formula commit does not
re-trigger the workflow. Shared composite actions live at
`coilysiren/agentic-os/actions/*`. This replaced the prior `.github/workflows`
release; building moved off GitHub Actions onto Forgejo.
