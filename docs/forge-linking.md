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

## See also

- [release.md](release.md) - canonical release flow.
