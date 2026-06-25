# ward agent: flags

Launch flags for `work`, `headless`, and `task`. See [docs/agent.md](agent.md)
for the verb family.

## Flags

`work` carries the container bring-up launch flags: `--aws`, `--detach`,
`--tag`/`--image`, `--ward-source`, `--no-pull`, `--branch` to override the
`issue-<N>` default, and `--repo owner/name` (repeatable; `--with-repo` is the
legacy alias, ward#280) to grant extra writable repos cloned full alongside the
issue's repo (multi-repo runs, [container-multi-repo.md](container-multi-repo.md)). `--print` resolves the issue and renders the seeded prompt +
docker plan without injecting the push token or running docker - the dry-run
preview, safe with no docker daemon up. `--force` skips the local + remote
concurrency reservation checks (see [docs/agent-reservation.md](agent-reservation.md)). `headless` and `task` swap `--detach`
(both always detach) for `--no-preflight`, which skips the autonomous pre-flight
([docs/agent-preflight.md](agent-preflight.md)) and detaches immediately.

## Quiet launch for detached runs (ward#306)

A detached launch (`headless`, `task`, any `--detach`) is not watched, so
docker's chatter below the verdict is noise: the pull status lines, the `docker
scout` footer, and the container-id hash `docker run -d` echoes. Those runs now
drop it (`DOCKER_CLI_HINTS=false` plus a swallowed stdout). An **interactive**
run streams docker unchanged.

## `--details` (ward#167)

`work` and `headless` take `--details "<note>"`: extra operator instructions
woven into the run at dispatch, for when the issue text isn't the whole story
("I actually want you to do it like this..."). The note rides as a final
paragraph of the **seeded prompt** - marked as added via `--details` and flagged
**authoritative over the issue text where they conflict** - so a single line can
steer or correct the run without editing the issue. It is also folded into the
**pre-flight read**, so the feasibility verdict accounts for the steer rather
than judging the bare issue. It shows up in `--print` (it's part of the rendered
seed). `task` has no `--details`: its `--instructions` already *are* the full
brief, so there's nothing separate to layer on.

## `--new-tab`: the sidequest spawn (ward#174)

`work` takes `--new-tab`: instead of launching the container attached to the
current terminal, it **spawns the work into its own Warp tab**. This is the
sidequest path - fan a tangent off into its own session without leaving the one
you're in - and the successor to the retired `ward dispatch interactive` Warp
seam.

The mechanics are deliberately thin. `--new-tab` resolves and validates the ref
first (the same exists/open/trusted gate as a normal run, so a bad ref fails
before any tab opens), then writes a tiny `{schema_version, ref, mode, title}`
JSON entry to a FIFO queue dir (`/tmp/ward-agent-queue`, mode 0600) and fires
`open warppreview://tab_config/claude-agent-work`. The agentic-os shim of that
name pops the oldest queue entry and runs `ward agent work <ref> --driver <mode>` in the
fresh tab - so the whole payload is the ref + mode, and the container does its
own fresh clone. The unix-nanos filename prefix gives each back-to-back spawn
its own tab without racing on a shared scratch file.

Overrides: `--channel preview|stable` (which Warp build to fire into, default
preview), `--surface tab|window` (new tab in the active window vs a fresh
window), `--launch-name` and `--queue-dir` (must match what the shim reads).
`--print` renders the resolved ref, the in-tab command, the Warp URL, and the
queue entry without writing or firing anything. If `open` fails, ward leaves the
queue entry in place and prints the `ward agent work <ref> --driver <mode>` command to
paste in a tab by hand. The agentic-os Warp configs and the shim live under
`warp/` in that repo.

## See also

- [docs/agent.md](agent.md) - the `ward agent` verb family and usage.
- [docs/container.md](container.md) - the container bring-up flags `work` carries.
