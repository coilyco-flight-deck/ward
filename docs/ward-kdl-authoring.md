---
doc_goal: Keep the authoring workflow short and current so a reader knows when a guardfile is needed and how a local ward-kdl rebuild happens.
---
# ward-kdl authoring

Most repos do not need a guardfile.

- If you only use `ward exec` and `ward git`, `.ward/ward.yaml` is enough.
- You need a guardfile when you are authoring or shipping a custom operator
  surface.

## Authoring loop

1. Edit the guardfile or config source.
2. Rebuild the generated surface.
3. Re-run the release-era checks.

`ward-kdl` compiles the guardfile into the audited surface that `ward` embeds.

## What the loop is for

- to change the verbs a release binary ships.
- to change the auth or transport shape behind a generated operator surface.
- to keep the generated surface bounded before runtime.

## What it is not for

- day-to-day repo development.
- changing `.ward/ward.yaml` for a consumer repo.
- hand-writing a giant command tree in Go.

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
- [ward-kdl-in-ward.md](ward-kdl-in-ward.md) - auto-mounting exec guardfiles.
