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

## Formula bump jobs

Two jobs rewrite the formula `url` line after a release:

- **bump-tap-formula** - rewrites `Formula/ward.rb` in the centralized
  flight-deck tap (`coilyco-flight-deck/homebrew-tap`), where
  `brew install coilyco-flight-deck/tap/ward` reads from. Runs on the dedicated
  `tap-writer` runner (`infrastructure/deploy/forgejo-runner-tap-writer.yml`): a
  host executor whose system git credential helper supplies the tap-write token
  only when git asks (`action=get`). The token never enters the job env, logs,
  or an Actions secret. The host executor has no node, so every step is pure
  shell - no JS `uses:` actions can run there.
- **bump-formula** - in-repo fallback `Formula/ward.rb` on ward's own forgejo
  `main`. Deprecated alongside the direct-repo tap; drop next cycle.

Both bumps carry the `[skip ci]` marker so the formula commit does not
re-trigger the workflow. Shared composite actions live at
`coilysiren/agentic-os/actions/*`. This replaced the prior `.github/workflows`
release; building moved off GitHub Actions onto Forgejo.
