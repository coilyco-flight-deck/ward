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
URL. Any appended query string (`?thing=stuff`) or hash fragment
(`#issuecomment-149`) is ignored, so a URL copied straight from the browser works
unedited.

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
   ending on a `GO` / `NO-GO: <reason>` line. ward runs this as a one-shot on the
   host (`claude -p`, or `goose run -t` for the goose mode), echoes the read to
   your terminal, and parses that final verdict line
   (markdown bold, bullets, and quote markers are tolerated; the last verdict line
   wins). The read is **issue-text-only**: the real run happens in a fresh clone
   of the issue's repo inside the container, so the prompt tells the agent the host
   cwd is unrelated scratch and to judge feasibility from the issue alone, never
   from whatever files are in the local tree. ward also runs the read in a neutral
   empty temp dir, **not the dispatch cwd** (ward#169), so a coding agent that
   ignores that instruction and walks the working tree finds nothing there to
   mistake for the clone - this is what stops a read dispatched from one repo's
   checkout from false-flagging `WRONG-REPO` because the issue's files look
   "missing" locally. Both levers are belt-and-suspenders; either alone kills the
   false gate.
2. On **GO** - or any read ward can't pin to an explicit NO-GO - the detached run
   launches. The bias is to proceed: only the agent itself saying "don't" blocks.
3. On **NO-GO** ward launches nothing and instead **posts a comment on the issue**
   with the reason, the full read (folded away), and how to re-dispatch. The work
   lands back in front of a human rather than failing silently.
4. On **WRONG-REPO** (ward#159) - the agent judged, from the issue text alone,
   that the work plainly belongs in a *different* repo - ward **blind-fires** a
   fresh issue into that repo and launches nothing here. See below.

### WRONG-REPO blind-fire (ward#159)

Sometimes the pre-flight read makes it obvious the issue was filed in the wrong
place - an ops verb that belongs on `coily`, an engine change that belongs on
`cli-guard`. The agent can end its read with `WRONG-REPO: owner/repo - <what to
file there>` instead of GO/NO-GO. The point is to **not burn cycles searching**:
the verdict comes from the issue text alone (the prompt tells the agent not to go
digging), and ward acts on it cheaply:

- It **blind-fires** a new issue into the named repo, reusing the source issue's
  text verbatim plus the routing reason and a provenance footer - no search, no
  second agent run. The new issue is flagged "filed blind ... confirm it fits
  before working it," since nobody looked at the target repo first.
- It **comments on the original issue** pointing at the freshly-filed one, with
  the full read folded away, and notes how to override (`--no-preflight`) if the
  routing is wrong.
- Nothing launches on either side. A human (or a later `ward agent` run) picks up
  the routed issue.

Guardrails: the target repo must be in ward's primary-org trust set (the same
gate `work` applies to its own owner), and it can't be the issue's own repo. If
the agent names an untrusted repo, no usable `owner/repo`, or the same repo, the
verdict degrades to a **NO-GO bounce** so a human routes it instead.

The check is skipped when there is no terminal (scripted / piped dispatch), on
`--print` (a dry run), and with `--no-preflight` (the escape hatch for a run
launched from a TTY that you still want to fire blind - it also re-dispatches a
NO-GO issue once you've decided it's good to go). The gate runs for both full
carry-to-merge harnesses, **claude and goose**, which are kept at parity here so
a rapidly-dispatched goose chore is feasibility-checked the same way (ward#148);
goose answers via `goose run -t`, claude via `claude -p`. Modes with no host
one-shot wired yet (`codex`/`qwen`), a host without the agent binary, or a read
that doesn't complete all **proceed** rather than block, since none of those is
the agent declining the work (and the reaper still backstops residual work).

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

### `--details` (ward#167)

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
