# Auto-mounting ward-kdl exec guardfiles into `ward`

ward#284 wires the exec-dialect ward-kdl guardfiles into the `ward` binary as a
**general delegation mechanism**, replacing the one-off graft pattern. Before
this, the only exec surface reachable through `ward` was the hand-grafted
`graftForgejoAdminExec` slice (cmd/ward/ops.go); every other guardfile -
including `ward-kdl.git.guardfile.kdl` - compiled only into the standalone
`ward-kdl` binary and was reachable nowhere through `ward`.

## What mounts

Every **exec-dialect** guardfile (`cmd/ward-kdl/ward-kdl.<area>.guardfile.kdl`
with an `exec <bin>` block) is grafted onto the `ward` command tree at the path
its `wrap` block names. The leading `ward-kdl` token maps to the `ward` root and
is dropped, so the rest of the `wrap` path becomes the mount path:

- `wrap ward-kdl docker` -> `ward docker`
- `wrap ward-kdl ops aws` -> `ward ops aws` (grafted beside the existing `ops` group)
- `wrap ward-kdl agents claude` -> `ward agents claude`

Today that lights up `ward docker`, `ward ops {aws,kubectl}`, and
`ward agents {aider,claude,codex,goose,ollama,opencode}` - surfaces that were
dark through `ward` before.

Spec-dialect guardfiles (forgejo, trello, tailscale, glitchtip, signoz, glama,
skillsmp) are **not** auto-mounted here: they need a spec lock + auth and ride
the `specverb` path (`ward ops forgejo`, cmd/ward/ops.go).

## How it works

`cmd/ward/wardkdl_exec.go` embeds the exec guardfiles (mirrored into
`cmd/ward/execassets/` by `make sync-exec-assets`, since `go:embed` cannot reach
the sibling `cmd/ward-kdl/` dir), parses each with `execverb.Parse`,
`execverb.Build`s its group, and grafts it onto the root - creating shared
intermediate groups (`ops`, `agents`) once. `main.go` calls `mountWardKdlExec`
before the verb-fallback set is read, so the new top-level groups (`docker`,
`agents`) count as real verbs. Every leaf wraps through ward's audit pipeline, so
each call writes one JSONL audit row and the wrapped binary owns its own
credentials. `env` values resolve lazily at exec time (e.g. ollama's
`OLLAMA_HOST` from SSM), so mounting never touches a token source.

Adding a new exec guardfile + `make build-ward-kdl` is the only step to grow the
surface - no per-guardfile Go graft.

## Collisions: hand-written surfaces win

Two exec guardfiles name a path a hand-written `ward` command already owns:

- `wrap ward-kdl git` -> `ward git` (cmd/ward/git.go)
- `wrap ward-kdl pkg brew` -> `ward pkg brew` (cmd/ward/pkg.go)

The mount **skips** a guardfile whose leaf is already taken, leaving the
hand-written command in place. This is deliberate: the hand-written `ward git`
carries mutating, concurrency-safe verbs (`commit`, `add`, `push`) the read-only
git guardfile does not, and `ward pkg brew` is jailed with scoped verbs. Both
guardfiles stay reachable through the standalone `ward-kdl` binary. Reconciling
them with their guardfiles (e.g. routing a future `git clone` repo-allowlist
guard through `ward git`) is left to those surfaces' own follow-ups.

The forgejo admin/doctor remote-exec slice is a separate special case: it
declares `wrap ward ops forgejo` (not `ward-kdl ...`) and is grafted onto the
`specverb` forgejo group so REST and remote-exec share one command. It is not
part of this auto-discovery set. See [ops-forgejo-admin](ops-forgejo-admin.md).

## Drift

`execassets_test.go` fails the build when the embedded mirror drifts from the
canonical exec-dialect guardfiles - a missing file, a byte mismatch, an extra
file, or a spec-dialect file that slipped in. Re-sync with `make
sync-exec-assets` (folded into `make build-ward-kdl`).

## See also

- [ward-kdl.md](ward-kdl.md) - what `ward-kdl` is: the build-time generator.
- [ops-forgejo-admin.md](ops-forgejo-admin.md) - the one-off graft this generalizes.
- [ward-kdl-surface.md](ward-kdl-surface.md) - the full ward-kdl verb surface.
- [git-verbs.md](git-verbs.md) - the hand-written `ward git` surface that wins the collision.
