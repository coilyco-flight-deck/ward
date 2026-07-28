---
doc_goal: Describe the audited git surface as a release-era user contract, not as a moving issue log.
---
# ward git

`ward git` wraps the git commands ward is willing to expose.

- `commit` is concurrency-safe.
- `clone` is destination-gated.
- network verbs keep the configured auth wiring.
- all verbs stay audited.

## See also

- [git-clone.md](git-clone.md) - clone-specific rules.
- [exec-verb.md](exec-verb.md) - the gated repo surface.
- [config-source.md](config-source.md) - where git auth and forge defaults come from.

## Typical commands

- `ward git status`
- `ward git commit -m ...`
- `ward git fetch`
- `ward git push`

The point is not to replace git. It is to make the supported git paths
auditable and consistent.
