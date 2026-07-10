---
doc_goal: Keep the Forgejo operator surface as one concise guide after the supporting issue-slice pages were removed.
---
# ops forgejo

`ward ops forgejo` is ward's Forgejo surface.

- It wraps the supported Forgejo REST calls.
- It is the canonical operator path for ward's own repo.
- It is an edge surface. Core `ward agent` dispatch uses typed Go adapters.
- The embedded surface is what the binary ships.

## What it replaces

- The old in-binary mount doc.
- The small view/quiet/admin split pages.
- The graft-removal consult page.

## What it covers

- repo and org lookups.
- issue and PR management.
- release and label operations.
- the limited admin-side verbs ward exposes to itself.

The details live in the generated surface when you are working in the build
layer. The release binary only needs the stable user-facing contract here.
Runtime config source changes can reshape this operator surface without
changing the core agent control plane.

## Why it still gets a page

Forgejo is ward's canonical repo and issue tracker. That makes the Forgejo
surface the one operator API most readers need to understand first.

## See also

- [ward-kdl-surface.md](ward-kdl-surface.md) - the generated family overview.
- [release.md](release.md) - the release pipeline that publishes ward.
