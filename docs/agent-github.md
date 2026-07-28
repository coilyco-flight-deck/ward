---
doc_goal: Keep the GitHub token-source anchor stable after the old page was collapsed.
---
# agent github

This page is the durable anchor for GitHub token resolution and host selection.

- It covers env, `gh`, and App-based token sources.
- It keeps GitHub separate from Forgejo's canonical release path.
- The token is resolved on the host from explicit user-provided sources.
- The shipped GitHub control plane is the issue-thread surface plus PR-context
  reads; Forgejo-native PR merge/status/backpressure verbs stay Forgejo-only
  until GitHub grows matching adapters.

## See also

- [compat-surface.md](compat-surface.md) - shipped provider and token-source matrix.
- [forgejo-token-audit.md](forgejo-token-audit.md) - the raw token read surface.
