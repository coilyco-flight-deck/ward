---
doc_goal: State the no-mount boundary after the Ward to Aguard cutover.
---
# generated surfaces in Ward

Generated guardfiles do not mount into Ward.

The former auto-mount resolved operator configuration during every Ward startup.
A stale edge reference could therefore break help, `ward agent`, and other
native paths. The cutover removes that coupling.

Use `aguard ops ...` inside the current AOS image for generated operator work.
Ward's hand-written `agent`, `container`, `exec`, `git`, and audit paths stay
native and do not load Aguard configuration.
