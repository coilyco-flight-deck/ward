# warded execution: lifecycle, roles, /workspace vs /substrate

Reference for [`../SKILL.md`](../SKILL.md). Written from ward's `docs/`; read the cited
source when a detail matters.

## The lifecycle

One run = one **ephemeral, least-access container** that carries a single issue start to
merge, then is thrown away (`docs/container.md`):

- **Fresh clone inside, never the host.** The working tree is a clone pulled *in* the
  container (cached via a shared `ward-gitcache` bare mirror); the host's repo tree is
  never touched. Many run at once, each uniquely named (`ward-<repo>-<rand>`).
- **Least access.** The only default host bind is the cwd (read-only) plus ward's
  entrypoint/doctrine; `~/.aws` (`--aws`) and the host tailnet route (`--tailnet`) are
  opt-in.
- **The reaper backstop** (`docs/container-reap.md`). On *every* exit (clean, crash,
  Ctrl-C) `ward container reap` fires as a `trap ... EXIT`, after the agent's permissions
  are out of the loop. It commits loose work, rebases onto the latest `main`, then either
  **pushes to `main`** (clean diff + clean integration) or **salvages** - pushes the work
  to a durable `ward-salvage/<id>` branch and files a `[ward-salvage]` issue. Salvage is
  the degraded outcome; the agent's job is to leave the reaper nothing to do. The reaper
  *verifies* `--repo` grants landed but never pushes them to `main`.
- **keep-10 retention** (`docs/container-cleanup.md`). Exited containers keep their
  writable layer until removed; each launch sweeps the exited `ward.container=1` tail,
  keeping the 10 most recent for `docker logs` post-mortem. Work is always preserved to the
  remote before exit, so a swept container loses only its log.

## The roles (startup roster, ward#347, ward#353)

Three roles, keyed on the role word and the argument type (`docs/agent.md`):

- **`engineer`** - implements a ticket end to end (implement → commit → merge → push →
  `closes #N`). A bare ref with no role word *is* an engineer carry. Detached only
  (ward#356). Trust-gated owner, `bypassPermissions`. `docs/agent-engineer.md`.
- **`director`** - attached backlog supervisor: dispatches engineers, polls `WARD-OUTCOME`
  markers, drains the lane, and on drain (or before the first drain) **surfaces a read-only
  scope + dispatch session** (`WARD_READONLY=1`) that reads the clone, **files + dispatches**
  work, but **cannot push this clone** - capture-and-dispatch is an obligation, not a "may".
  ward#353 folded the old standalone `architect` role into this surface. `docs/agent-director.md`,
  `docs/agent-surface.md`.
- **`advisor`** - answers, writes no code (a ref comments, freeform answers inline).

**What read-only enforces** (the director's surface): a composed restriction block, the git credential
helper dropped, the `origin` push URL repointed to a dead `no-push://` target, a `pre-push`
message hook, and the reaper short-circuiting salvage on `WARD_READONLY`. Local `git
commit` still works; nothing leaves the clone. The dispatch token is the *same* bot token,
so the no-push rule is still partly a convention (a dispatch-only credential is ward#318).

## `/workspace` (work) vs `/substrate` (read-only reference)

Two trees, split by **role, not by which repos exist where** (`docs/container-substrate.md`):

- `/workspace/<name>` - **authoritative for work**: edits, commits, the feature branch, the
  push. The target and any `--repo` grant live here.
- `/substrate/<name>` - **read-only reference**: doctrine, skills, cross-repo contracts, the
  dev/ops CLIs, warmed regardless of target. Read a convention here; never work here.

The same repo can sit in **both** trees (target also on the substrate manifest), hydrated
from the one shared mirror at the same HEAD. That overlap is expected: read from either,
**act only on `/workspace`**. Once you edit, the `/substrate` copy is a stale snapshot.
