# ward agent: flags

Launch flags for the `engineer` carry (ward#347). See [docs/agent.md](agent.md)
for the roster.

## Flags

The engineer carry brings the container bring-up launch flags: `--aws`, the
mutually-exclusive tailnet routes `--host-net` (native-Linux host route; no-op +
warns elsewhere, ward#332; [agent-host-net.md](agent-host-net.md)) and
`--ts-sidecar` (the Docker Desktop route to the standing mac-proxy box, ward#349;
[agent-ts-sidecar.md](agent-ts-sidecar.md)) - `--host-net` implies `--aws`,
`--ts-sidecar` does not,
`--tag`/`--image`/`--ward-version` (pin once via `WARD_AGENT_{TAG,IMAGE,VERSION}`,
ward#312), `--ward-source`, `--no-pull`, `--branch` to override the
`issue-<N>` default, and `--repo owner/name` (repeatable; `--with-repo` is the
legacy alias, ward#280) to grant extra writable repos cloned alongside the
issue's repo ([container-multi-repo.md](container-multi-repo.md)). `--print` renders the seed +
docker plan with no push token - a dry run. `--force` skips the
concurrency reservation checks (see [docs/agent-reservation.md](agent-reservation.md)). The carry
**always detaches** (ward#356): there is no attach surface - the old `--watch` (`-w`) and
its `--new-tab` Warp spawn are retired; interactive work funnels to the
[director](agent-director.md). The carry also takes `--no-preflight`, which skips the
autonomous pre-flight ([docs/agent-preflight.md](agent-preflight.md)) and detaches immediately.

## Quiet launch for detached runs (ward#306, ward#322)

A detached launch (the engineer carry, always detached now; ward#356) isn't watched, so docker's
chatter is dropped: pull lines, the `docker scout` footer, the container-id hash
(`DOCKER_CLI_HINTS=false` plus a swallowed stdout). The pull is the one exception (ward#322): silencing it hid
slow/mid-push-registry stalls, so a detached pull names itself up front and beats
a periodic `still pulling` heartbeat, then falls back to the local image.

## `--details` (ward#167)

The engineer carry's **ref mode** takes `--details "<note>"`: extra operator instructions
woven in at dispatch as a final paragraph of the **seeded prompt**, flagged
**authoritative over the issue text** where they conflict - so a single line steers the
run without editing it. It is also folded into the **pre-flight read** and shows up in
`--print`. The **freeform mode** has no `--details` - its `--instructions` already *are*
the full brief.

## Retired: `--watch` and `--new-tab` (ward#356)

Engineer once had an attach-and-pair surface - `--watch` (`-w`, the old `work`) ran the
container attached to your terminal, and `--new-tab` (ward#174) spawned that attached carry
into its own Warp tab (the sidequest path). Both are **gone** (ward#356): engineer is
detached / autonomous only, and all interactive agent work funnels to the
[director](agent-director.md) (the managed shell). The flags error as unknown.

## See also

- [docs/agent.md](agent.md) - the `ward agent` roster and usage.
- [docs/container.md](container.md) - the container bring-up flags the engineer carry brings.
