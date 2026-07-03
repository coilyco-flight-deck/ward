---
doc_goal: Give an operator the full trimmed launch-flag surface for the engineer role - visible, hidden, and retired - so each flag's effect on the container boundary (network escalation, image pinning, reaper downgrade guard, dry run) is actionable, not just enumerated.
---
# ward agent: flags

Launch flags for the `engineer` role. See [docs/agent.md](agent.md)
for the roster.

## The flag surface (trimmed ~24 -> ~10)

The shared launch helpers show ~10 visible flags: the positional ref/task, `--driver`,
`--repo`, `--details`, `--aws`, `--tailnet`, `--print`, `--force`, `--no-preflight`, and
(engineer freeform) `--instructions-file`. The trim lands in the shared helpers, so it
applies to the engineer, director, and advisor surfaces at once.

`--tailnet` joins the container to the tailnet to reach tailnet-only hosts like
`<ollama-host>`, **auto-selecting the mechanism by platform**: the native-Linux host route
(`--network=host`, [agent-host-net.md](agent-host-net.md)) or the SOCKS5 sidecar
on Docker Desktop ([agent-ts-sidecar.md](agent-ts-sidecar.md)). It **implies
`--aws`**. `--repo owner/name` (repeatable) grants extra writable repos
([container-multi-repo.md](container-multi-repo.md)). `--print` is a dry run. `--force`
skips the reservation checks ([agent-reservation.md](agent-reservation.md)) and
`--no-preflight` skips the pre-flight ([agent-preflight.md](agent-preflight.md)). The engineer
**always detaches**.

### Hidden but functional

* `--tailnet-mode auto|host-net|sidecar` - pin the mechanism (a non-auto value implies `--tailnet`).
* `--tag` / `--image` / `--ward-version` - pin the image, env-backed via `WARD_AGENT_{TAG,IMAGE,VERSION}`.
* `--allow-ward-downgrade` - permit a `--ward-version` pin **older** than this host's ward. Refused by default: the pin is what the container's in-process reaper runs, and an older reaper can reproduce already-fixed lost/false-salvage bugs. An equal-or-newer pin needs no flag.
* `--ward-source` - build ward from a local checkout (development-only).
* `--branch` - override the `issue-<N>` branch default. `--no-pull` - reuse the cached image.

### Deleted

* `--instructions` / `-i` - use the freeform positional, or `--instructions-file` in DIRECT mode.
* `--with-repo` - the alias of `--repo` is gone (advisor and director keep their own separate `--with-repo`).
* `--go-bootstrap` - the experimental toggle left the surface.

## Quiet launch for detached runs

A detached launch (the engineer, always detached now) isn't watched, so docker's
chatter is dropped: pull lines, the `docker scout` footer, the container-id hash
(`DOCKER_CLI_HINTS=false` plus a swallowed stdout). The pull is the one exception: silencing it hid
slow/mid-push-registry stalls, so a detached pull names itself up front and beats
a periodic `still pulling` heartbeat, then falls back to the local image.

## `--details`

The engineer's **ref mode** takes `--details "<note>"`: extra operator instructions
woven in at dispatch as a final paragraph of the **seeded prompt**, flagged
**authoritative over the issue text** where they conflict - so a single line steers the
run without editing it. It is also folded into the **pre-flight read** and shows up in
`--print`. The **freeform mode** has no `--details` - its positional text (or
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
