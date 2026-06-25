# ward agent: flags

Launch flags for `work`, `headless`, and `task`. See [docs/agent.md](agent.md)
for the verb family.

## Flags

`work` carries the container bring-up launch flags: `--aws`, the
mutually-exclusive tailnet routes `--host-net` (native-Linux host route; no-op +
warns elsewhere, ward#332; [agent-host-net.md](agent-host-net.md)) and
`--ts-sidecar` (the Docker Desktop sidecar, ward#333, by-name tower reach in
ward#337; [agent-ts-sidecar.md](agent-ts-sidecar.md)) - both imply `--aws`, `--detach`,
`--tag`/`--image`/`--ward-version` (pin once via `WARD_AGENT_{TAG,IMAGE,VERSION}`,
ward#312), `--ward-source`, `--no-pull`, `--branch` to override the
`issue-<N>` default, and `--repo owner/name` (repeatable; `--with-repo` is the
legacy alias, ward#280) to grant extra writable repos cloned alongside the
issue's repo ([container-multi-repo.md](container-multi-repo.md)). `--print` renders the seed +
docker plan with no push token - a dry run. `--force` skips the
concurrency reservation checks (see [docs/agent-reservation.md](agent-reservation.md)). `headless` and `task` swap `--detach`
(both always detach) for `--no-preflight`, which skips the autonomous pre-flight
([docs/agent-preflight.md](agent-preflight.md)) and detaches.

## Quiet launch for detached runs (ward#306, ward#322)

A detached launch (`headless`, `task`, any `--detach`) isn't watched, so docker's
chatter is dropped: pull lines, the `docker scout` footer, the container-id hash
(`DOCKER_CLI_HINTS=false` plus a swallowed stdout). An **interactive** run streams
it unchanged. The pull is the one exception (ward#322): silencing it hid
slow/mid-push-registry stalls, so a detached pull names itself up front and beats
a periodic `still pulling` heartbeat, then falls back to the local image.

## `--details` (ward#167)

`work` and `headless` take `--details "<note>"`: extra operator instructions
woven into the run at dispatch, for when the issue text isn't the whole story.
The note rides as a final
paragraph of the **seeded prompt**, flagged **authoritative over the issue text
where they conflict** - so a single line can steer the run without editing it. It is also folded into the
**pre-flight read**, so the feasibility verdict accounts for the steer. It shows
up in `--print` (it's part of the rendered
seed). `task` has no `--details`: its `--instructions` already *are* the full
brief, so there's nothing separate to layer on.

## `--new-tab`: the sidequest spawn (ward#174)

`work` takes `--new-tab`: instead of launching the container attached to the
current terminal, it **spawns the work into its own Warp tab**. This is the
sidequest path - fan a tangent off into its own session without leaving the one
you're in.

The mechanics are thin. `--new-tab` validates the ref first (the same
exists/open/trusted gate, so a bad ref fails before any tab opens), then writes a
tiny `{schema_version, ref, mode, title}` JSON entry to a FIFO queue dir
(`/tmp/ward-agent-queue`, mode 0600) and fires
`open warppreview://tab_config/claude-agent-work`. The agentic-os shim of that
name pops the oldest entry and runs `ward agent work <ref> --driver <mode>` in
the fresh tab. A unix-nanos filename prefix gives each back-to-back spawn its own
tab without racing on a shared scratch file.

Overrides: `--channel preview|stable` (which Warp build to fire into, default
preview), `--surface tab|window` (new tab in the active window vs a fresh
window), `--launch-name` and `--queue-dir` (must match the shim).
`--print` renders the resolved ref, the in-tab command, the Warp URL, and the
queue entry without writing or firing anything. If `open` fails, ward leaves the
queue entry in place and prints the `ward agent work <ref> --driver <mode>`
command to paste by hand. The agentic-os Warp configs and the shim live under
`warp/` in that repo.

## See also

- [docs/agent.md](agent.md) - the `ward agent` verb family and usage.
- [docs/container.md](container.md) - the container bring-up flags `work` carries.
