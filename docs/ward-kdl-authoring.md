---
doc_goal: Keep the authoring workflow short and current so a reader knows when a guardfile is needed and how a local ward-kdl rebuild happens.
---
# ward-kdl authoring

Most repos do not need a guardfile.

- If you only use `ward exec` and `ward git`, `.ward/ward.yaml` is enough.
- AOS owns guardfiles for a custom operator surface and ships the result as
  `aguard`; Ward does not compile those surfaces.

## Authoring loop

1. Edit the AOS guardfile or spec source.
2. Rebuild the AOS image and Aguard.
3. Re-run the AOS release checks.

Specgen compiles the guardfile into the audited Aguard surface.

## What the loop is for

- to change the Aguard verbs the AOS image ships.
- to change the auth or transport shape behind an Aguard operator surface.
- to keep the generated surface bounded before runtime without coupling it to Ward.

## What it is not for

- day-to-day repo development.
- changing `.ward/ward.yaml` for a consumer repo.
- changing Ward's native agent, container, or dev-verb surface.

## Practical notes

- the build step is the source-of-truth transition.
- if the generated output changes, the release docs should still explain the
  shipped behavior.
- the authoring doc should stay in sync with the surface and mount docs.
- per-area Markdown refs are generated output, not committed release docs.

## See also

- [.ward/ward-kdl/README.md](../.ward/ward-kdl/README.md) - the generated outpost copy.
- [ward-kdl.md](ward-kdl.md) - the build-time layer.
- [ward-kdl-surface.md](ward-kdl-surface.md) - the generated API surface.
- [ward-kdl-in-ward.md](ward-kdl-in-ward.md) - Ward's no-mount boundary.
