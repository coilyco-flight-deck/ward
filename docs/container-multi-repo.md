---
doc_goal: Keep the multi-repo anchor stable after the old page was collapsed.
---
# container multi repo

This page is the durable anchor for multi-repo container behavior.

- The primary checkout remains at `/workspace/<repo>` so the agent starts in a
  stable, obvious cwd. Every extra writable or read-only context repository is
  at `/workspace/<owner>/<repo>`, so repositories with the same basename (for
  example two organization `.github` repositories) can coexist.
- It covers extra read-only or granted repos mounted beside the target.
- It keeps the host-side gitcache seeding comments readable.

## See also

- [container-substrate.md](container-substrate.md) - `/substrate` and grants.
- [container-lifecycle.md](container-lifecycle.md) - launch and teardown.
