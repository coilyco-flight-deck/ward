---
doc_goal: Give an operator the trimmed engineer launch surface - visible, hidden, and retired - so each flag's effect on the container boundary is actionable.
---
# ward agent: flags

Launch flags for the `engineer` role. See [docs/agent.md](agent.md)
for the roster.

## The flag surface (trimmed ~24 -> ~8)

The shared launch helpers show ~10 flags: the positional ref/task, `--harness`
(or `--agent`), `--repo`, `--details`, `--config`, `--print`, `--force`,
`--skip-preflight`, and (engineer freeform) `--instructions-file`.

`--repo owner/name` (repeatable) grants extra writable repos
([container-multi-repo.md](container-multi-repo.md)). `--print` is a dry run.
`--force` skips the reservation checks ([agent-reservation.md](agent-reservation.md)).
`--config` (repeatable) overrides the agent's model-context config
([agent-config-overrides.md](agent-config-overrides.md)). The engineer **always detaches**.
`--skip-review` disables the in-container review gate, and `--skip-preflight` skips the
host pre-flight and launch-adjacent startup probes. See [agent-preflight.md](agent-preflight.md)
for scope and safety gates. `--no-review-gate` and `--no-preflight` stay accepted as aliases.

### Host/cloud capability is a per-role guardfile set, not a flag

`~/.aws` and the tailnet are no longer first-class flags: a role's capability is
guardfile membership in `ward-kdl.fleet.kdl`'s `roles` block (advisor holds the
live-observe set, engineer/director hold none). See [agent-capability.md](agent-capability.md).

### Hidden but functional

* `--aws` / `--tailnet` - deprecated back-compat aliases, hidden for one release; they force the capability on. See [agent-capability.md](agent-capability.md).
* `--tailnet-mode auto|host-net|sidecar` - pin the tailnet mechanism (non-auto force-joins).
* `--tag` / `--image` / `--ward-version` - pin the image, env-backed via `WARD_AGENT_{TAG,IMAGE,VERSION}`.
* `--allow-ward-downgrade` - permit a `--ward-version` pin **older** than this host's ward. Refused by default: the pin runs the container's in-process reaper, and an older one can reproduce fixed lost/false-salvage bugs.
* `--ward-source` - build ward from a local checkout (development-only).
* `--branch` - override the `issue-<N>` branch default. `--no-pull` - reuse the cached image.

### Deleted

* `--instructions` / `-i` - use the freeform positional, or `--instructions-file` in DIRECT mode.
* `--with-repo` - the alias of `--repo` is gone (advisor and director keep their own separate `--with-repo`).
* `--go-bootstrap` - the experimental toggle left the surface.

## Quiet launch for detached runs

A detached launch (the engineer) isn't watched, so docker's chatter is dropped: pull
lines, the `docker scout` footer, the container-id hash (`DOCKER_CLI_HINTS=false` plus
swallowed stdout). The pull is the exception: silencing it hid slow-registry stalls, so
it names itself and beats a `still pulling` heartbeat before falling back to local.

## `--details`

The engineer's **ref mode** takes `--details "<note>"`: extra operator instructions
woven in at dispatch as a final paragraph of the **seeded prompt**, flagged
**authoritative over the issue text** where they conflict. It is also folded into the
**pre-flight read** and shows up in `--print`. The **freeform mode** has no `--details` - its positional text (or
`--instructions-file`) already **is** the full brief.

## Retired: `--watch` and `--new-tab`

Engineer once had an attach-and-pair surface - `--watch` (`-w`, the old `work`) ran the
container attached to your terminal, and `--new-tab` spawned that attached run
into its own Warp tab (the sidequest path). Both are **gone**: engineer is
detached / autonomous only, and all interactive agent work funnels to the
[director](agent-director.md) (the managed shell). The flags error as unknown.

## See also

- [docs/agent.md](agent.md) - the `ward agent` roster and usage.
- [docs/container.md](container.md) - the container bring-up flags the engineer brings.
