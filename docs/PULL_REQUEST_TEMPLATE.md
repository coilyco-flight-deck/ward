<!--
ward is Forgejo-canonical and mirrored read-only to GitHub. GitHub is the
external front door for everyone except the maintainer: fork, push a branch,
open your PR here. A maintainer replays an accepted PR onto Forgejo `main` and
closes the matching Forgejo issue. If you are working directly in the canonical
repo, use Forgejo instead. See CONTRIBUTING.md.
-->

## Summary

<!-- What does this change do, and why? -->

## Related issue

<!-- Link the GitHub issue this addresses, e.g. Fixes #123. The internal
     Forgejo `closes #N` link is added by whoever carries the change across -
     you don't need it unless you're the maintainer working directly on
     Forgejo. -->

## Checklist

- [ ] I opened an issue first and referenced it above (see CONTRIBUTING.md).
- [ ] The change stays within ward's scope (the dev-verb gate / agent driver), not a personal-infra or downstream-repo verb.
- [ ] I ran the dev verbs locally: `ward exec build`, `ward exec test`, `ward exec vet`, `ward exec lint`.
- [ ] Docs updated where behaviour changed (`docs/`, `README.md`, `docs/FEATURES.md`).
