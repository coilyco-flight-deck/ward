# ward docker exec

`ward docker exec` is the guarded shell-into-a-run leaf.

- It is a `ward=true` gated path.
- It bypasses the normal detached agent launch.
- It exists for the narrow case where a run must be inspected directly.

## When to use it

- you need to inspect the live container.
- you need to reproduce a command inside the run environment.
- you do not want the detached agent workflow.

## When not to use it

- for normal implementation work.
- for repo policy changes.
- for general container administration.

The point is to keep the leaf narrow. A shell-into-run command is powerful, so
ward keeps it clearly separate from the normal agent launch path.

## See also

- [container.md](container.md) - the run box.
- [ward-kdl.md](ward-kdl.md) - the build-time layer.
