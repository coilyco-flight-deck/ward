# forge linking

Use the right forge target for the link.

- Same-repo links stay relative.
- Public contributor navigation points at GitHub.
- Canonical release and repo references point at Forgejo.

## Common cases

- README links to this repo use relative paths.
- issue links that ask outsiders to file bugs point at GitHub.
- release, tag, and canonical repo links point at Forgejo.

## Rule of thumb

- if the link is to this repo, keep it relative.
- if the link is for outside contributors, use GitHub.
- if the link is about the canonical repo, issues, releases, or install
  infrastructure, use Forgejo.

## Issue References

Ward-authored durable text must not rely on renderer-local issue shorthand.
Generated issue bodies, issue comments, dispatch artifacts, reaper output,
release notes, and seed prompts should use full issue URLs when the tracker
authority matters. Where a literal close target is required, use the qualified
same-repo form, such as `closes coilyco-flight-deck/ward#1501`, rather than a
bare `closes #1501`.

Docs should link issue references with full URLs, for example
`[ward#1501](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1501)`,
or use a fully qualified `owner/repo#N` only when the surrounding prose already
states the forge.

## See also

- [release.md](release.md) - canonical release flow.
