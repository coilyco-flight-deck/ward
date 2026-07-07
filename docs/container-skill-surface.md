---
doc_goal: Settle the substrate-vs-full-tree design for a ward container - why it does NOT rebuild the host's absolute-path skill symlink forest, what the in-container skill surface actually is (substrate slice read as docs, plus per-run `--repo` capability), and how host-only fleet scripts fence on `WARD_CONTAINER`.
---
# Container skill surface (substrate vs full tree)

The host harness builds an active skill surface by symlinking every on-disk org
dir's `.agents/skills/*` into `~/.claude/skills` (`mount-skills.sh`) and composing
the global CLAUDE.md from absolute `/Users/kai/projects/...` sources. A `ward
container` has no such tree: it fresh-clones one target
under `/workspace`, warms a fixed ~8-repo substrate slice read-only under
`/substrate` ([container-substrate.md](container-substrate.md)), and mounts any
`--repo` grants. This resolves the three decisions in
[ward#114](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/114),
whose body holds the full coupling map.

## Decision 1 - the surface is the substrate slice, read as docs

The container does **not** rebuild the symlink forest, and none should be added -
it would re-introduce the absolute-path fleet coupling this issue removes and key
off a `~/projects` tree that does not exist here. Instead the substrate repos land
under `/substrate/<name>` carrying their own `.agents/skills/` as **files to
read**. The core language / `kai-*` / `coding-*` / `agents-*` skills all live in
agentic-os + agentic-os-kai (both substrate), so the core surface survives. The
composer's read-these-first block ([ward#593](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/593))
is how an agent finds them without a forest.

Deliberately lost: `repo-<name>` pointers for the ~30 non-substrate repos (fleet
awareness a one-feature run never asks for), and their capability skills. The lever
to pull one back is **not** widening the shared substrate - it is `--repo
owner/name` ([container-multi-repo.md](container-multi-repo.md)), which clones that
repo full with its skills. `preclone-repos.txt` stays the shared slice.

## Decision 2 - fleet scripts are host-only, fenced on `WARD_CONTAINER`

The ~18 fleet-walk scripts (up-to-date.py, _fleet.py, fleet-pulse.sh,
build-catalog-graph.py, ...) assume the full tree. Inside a container they do not
fail - they walk 8 repos and report a partial fleet as whole. Every `ward
container` exports **`WARD_CONTAINER=1`** (`wardEnv` in
[container_compute.go](../cmd/ward/container_compute.go), with a matching
entrypoint default; [container-env.md](container-env.md)). A host shell never has
it, so a fleet script fences on it:

```sh
[ -n "${WARD_CONTAINER:-}" ] && { echo "control-node-only" >&2; exit 3; }
```

It is preferred over incidental signals (the always-set `WARD_CONTAINER_NAME`,
`/substrate`) because a fence should read off intent. ward owns the marker; the
guard clauses live in agentic-os-kai - the authoring-vs-rollout split.

## Decision 3 - the list is reconciled to the post-reduction kept set

The June 2026 reduction archived `coily`; it was dropped from
[`preclone-repos.txt`](../cmd/ward/containerassets/preclone-repos.txt) (and
ansible's `clone-substrate-repos.sh`). The reconciled 8: **image tier** -
agentic-os, cli-guard, infrastructure, ward, coilysiren/coilysiren; **cache
tier** - agentic-os-kai, deploy, lore. Non-substrate repos come per-run via
`--repo`, never by widening this slice.

## Follow-ups (agentic-os-kai)

- Add the `WARD_CONTAINER` guard preamble to the fleet-walk scripts. ward has
  landed the marker they key off.
- `mount-skills.sh` / `agent-compose` would need a path-portable rewrite to run in
  a container. The position here is the container does not need them
  (read-as-docs + `--repo` cover it), so this is only-if-a-gap-appears.

## See also

- [container-substrate.md](container-substrate.md) - the `/substrate` layer.
- [container-multi-repo.md](container-multi-repo.md) - `--repo`, the per-run lever.
