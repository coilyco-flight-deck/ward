# ward agent

`ward agent <name> work <issue>` is the short verb over [`ward container`](container.md)
for the common case: take a Forgejo issue and put an agent on it end to end. One
line replaces the full `container up <repo> --mode <m> --branch <b>` stack plus a
hand-written prompt.

## Usage

```bash
ward agent claude work coilyco-flight-deck/ward#98
ward agent claude work https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/98
ward agent claude headless coilyco-flight-deck/ward#98          # detached, fire-and-forget
ward agent codex  work coilyco-flight-deck/ward#98 --print      # resolve + show the plan, run nothing
```

`<name>` is the agent/mode (`claude|codex|qwen|goose`, the same context ladder as
`container up --mode`). The issue ref is `owner/repo#N` or a full Forgejo issue
URL.

## What `work` does

1. **Resolve + validate.** Parses the ref, then fetches the issue from Forgejo so
   a bad ref, a typo, or an untrusted owner fails *before* any container spins.
2. **Trust-gate.** The target is refused unless its owner is in ward's primary-org
   set, because the container runs under `bypassPermissions` - the same gate
   `ward dispatch` applies. A non-`open` issue warns but proceeds.
3. **Branch.** Derives `issue-<N>` as the feature branch (override with `--branch`).
4. **Launch.** Spins up an interactive `ward container` against a fresh clone of
   the repo, seeded with a "read the issue, then carry it to merge" prompt.

The seed prompt rides as the in-container agent's argv (the entrypoint's
`"$WARD_AGENT" "$@"`), so the agent opens already pointed at the issue. The
container doctrine
([AGENTS.container.md](../cmd/ward/containerassets/AGENTS.container.md)) supplies
the carry-to-merge autonomy and the reaper backstop; `work` only seeds the issue.

## work vs headless

- **`work`** (interactive) attaches the container to your terminal - you watch
  the agent and can step in. `--detach` backgrounds it.
- **`headless`** is fire-and-forget: it always detaches and runs the agent in
  print mode (`claude -p`, or `goose run -t <seed>` for the goose mode), so it
  works to completion non-interactively and exits into the reaper. For claude it
  **streams live progress** (one line per tool call + the result, via
  stream-json) to the container log - `docker logs <name>` / `ward container
  exec`; goose prints its own progress to that log - so it isn't silent until
  done. (Interactive `goose work` opens a bare `goose session`; the seed prompt
  is not auto-delivered into a session yet, so headless is the goose surface.)
  When dispatched from a terminal it first runs a **pre-flight check** (see
  below) - fire-and-forget: a GO launches the run, a NO-GO comments on the issue
  and launches nothing, with no prompt to answer.
- **`task`** files an issue from `--instructions` first, then runs the `headless`
  flow against it (carries to merge, `closes #N`). See [docs/agent-task.md](agent-task.md).

Both `headless` and `task` also install an [agent-only commit suite](agent-precommit.md)
(`closes-issue` + `conventional-commit`) that interactive `work` and humans never see.

## Headless pre-flight (ward#137, ward#147)

`headless` detaches into a fire-and-forget run nobody is watching, so when it is
**dispatched interactively** (a human at the terminal) ward inserts a quick
pre-flight *before* detaching. The gate is **fire-and-forget from your POV**
(ward#147): you launch and walk away, and ward acts on the agent's verdict with
no prompt to answer:

1. The agent gets a short prompt with the issue title + body and answers, in a
   sentence or two, whether it thinks it can carry the issue to merge unattended,
   ending on a `GO` / `NO-GO: <reason>` line. ward runs this as `claude -p` on the
   host, echoes the read to your terminal, and parses that final verdict line
   (markdown bold, bullets, and quote markers are tolerated; the last verdict line
   wins).
2. On **GO** - or any read ward can't pin to an explicit NO-GO - the detached run
   launches. The bias is to proceed: only the agent itself saying "don't" blocks.
3. On **NO-GO** ward launches nothing and instead **posts a comment on the issue**
   with the reason, the full read (folded away), and how to re-dispatch. The work
   lands back in front of a human rather than failing silently.

The check is skipped when there is no terminal (scripted / piped dispatch), on
`--print` (a dry run), and with `--no-preflight` (the escape hatch for a run
launched from a TTY that you still want to fire blind - it also re-dispatches a
NO-GO issue once you've decided it's good to go). Non-`claude` modes, a host
without the agent binary, or a read that doesn't complete all **proceed** rather
than block, since none of those is the agent declining the work (and the reaper
still backstops residual work).

`task` runs the **same pre-flight** (ward#149): it files the issue first, then
gives the same GO / NO-GO read before detaching. A NO-GO comments on the
just-filed issue and launches nothing, leaving a real issue a human can pick up
or re-dispatch with `headless ... --no-preflight`. It honors the same skips
(`--print`, `--no-preflight`, no terminal).

The reaper backstop salvages residual work if the agent crashes (it needs ward's
jail off in-container - the entrypoint exports `CLIGUARD_NO_SANDBOX=1`, cli-guard#153).
The happy path doesn't rely on it: the agent commits/merges/pushes itself per its
doctrine, finishing to a clean `main` push.

## Host stale-ward reminder (ward#143)

A `ward agent` run installs ward *inside* the container and logs its `ward
version` there. When the run is non-interactive - `headless`/`task` (always
detached) or `work --detach` - no human watches that log, so the cue that the
**host** ward binary is itself behind a release is lost. To keep that awareness,
ward does a best-effort check at the host dispatch moment (where the operator
still is): it fetches the latest `coilyco-flight-deck/ward` release tag and, if
the host binary is behind it, prints a two-line stderr reminder pointing at
[`ward upgrade`](../README.md).

The check is deliberately quiet and non-blocking: a `dev`/source build, no
network, an auth wall, or an unparseable tag all stay silent rather than guess,
and a 5s timeout means a slow Forgejo never holds up the dispatch. It is skipped
under `--print` (a pure offline dry run). It compares only the release tag, not
the in-container ward, because the container always pins/downloads its own.

## Credentials and user

claude runs **non-root** (uid 1000): it refuses `--dangerously-skip-permissions`
as root, so the entrypoint sets up as root then drops via `setpriv`. It
authenticates with your **Max/subscription login**, not an API key - ward
resolves the OAuth credential on the host and injects it into the container's
`~/.claude/.credentials.json` via the private `--env-file`, never in argv/audit.
`ANTHROPIC_API_KEY` stays unset so it can't shadow OAuth.

## Reservation (no double-work)

Before a container fires, the run **reserves the issue** so a second run never
works it at the same time - on this host or another:

- **Local file sentinel.** `~/.ward/agent-reservations/<owner>-<repo>-issue-<N>.json`
  records the container holding the issue. A fresh sentinel whose container is
  still running blocks a new run on the same host.
- **Remote Forgejo comment.** The run posts a marker comment (`🔒 Reserved by
  ward agent ...`) on the issue and refuses to start if it finds a fresh one
  already there - that's another host carrying the issue.

Both holds are **TTL-bounded** (2h): an older reservation is assumed dead and
reclaimed, so a crashed run never wedges an issue. The local sentinel is also
reclaimed the moment its container is no longer running. An attached `work` run
releases its sentinel when it returns; a detached run (`headless`, `task`,
`--detach`) leaves it for the container's lifetime, bounded by the TTL +
liveness check. Remote/network failures degrade to a warning - the local
sentinel still guards this host - so a transient Forgejo hiccup never blocks a
launch. `--print` reserves nothing (it's a dry run). `--force` skips both checks
to reclaim a stale or foreign hold.

## Flags

`work` carries the `container up` launch flags: `--aws`, `--detach`,
`--tag`/`--image`, `--ward-source`, `--no-pull`, and `--branch` to override the
`issue-<N>` default. `--print` resolves the issue and renders the seeded prompt +
docker plan without injecting the push token or running docker - the dry-run
preview, safe with no docker daemon up. `--force` skips the local + remote
concurrency reservation checks (see above). `headless` and `task` swap `--detach`
(both always detach) for `--no-preflight`, which skips the autonomous pre-flight
described above and detaches immediately.

## Container name

Where a bare `container up` names its container `ward-<repo>-<rand>`, an agent
run names it for the work it carries: `ward-<repo>-issue-<N>-<mode>-<rand>`. So
`docker ps` reads the repo, the issue, and the harness at a glance, and a host
driving several agents at once can tell them apart - the `<rand>` suffix still
keeps concurrent runs on the same issue from colliding. `task` shows the shape
as `ward-<repo>-issue-<N>-<mode>-<rand>` under `--print`; the real number lands
once the issue is filed.

## See also

- [docs/container.md](container.md) - the container model this wraps (ephemeral,
  fresh-clone-inside, least-access, reaper-backed).
- [docs/dispatch.md](dispatch.md) - the host-native sibling that fires `claude`
  against an issue in the canonical checkout instead of a fresh container.
