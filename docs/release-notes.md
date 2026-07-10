---
doc_goal: Keep the release-notes script anchored after the old release page was collapsed.
---
# release notes

This page is the durable anchor for `scripts/release-notes.sh`.

- It reshapes raw `git log` subjects into user-facing release notes.
- It keeps routine internal churn collapsed behind a details block.
- The release workflow feeds it the same compare range that the tagger uses.

## See also

- [release.md](release.md) - the release pipeline.
- [release-binaries.md](release-binaries.md) - the shipped artifacts.
