---
doc_goal: Keep the generated ward-kdl surface readable without the old per-area reference tree.
---
# ward-kdl surface

The generated surface is the audited command set built from a guardfile.

## Families

- `ops` - spec-driven APIs such as Forgejo.
- `docker` - the read-only Docker inspection surface.
- `agents` - the harness launchers.
- `pkg` - package-directory lookups.

## What matters

- The generated surface is what `ward` embeds.
- Missing verbs are compile-time omissions, not runtime accidents.
- The release binary only carries the surfaces that were built in.

## Read it like a map

- `ops` is the operator-facing tree.
- `docker` is the inspection tree.
- `agents` is the launcher tree.
- `pkg` is the lookup tree.

The important part is not the file list, it is the fact that the surface is
generated from the guardfile and therefore bounded before runtime.

## See also

- [ward-kdl.md](ward-kdl.md) - the build-time layer.
- [ward-kdl-in-ward.md](ward-kdl-in-ward.md) - the exec mounts.
