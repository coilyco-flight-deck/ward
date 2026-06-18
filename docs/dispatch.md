# ward dispatch

`ward dispatch` fires `claude` against a real, already-open issue. It is the
contributor-facing home for the dispatch subsystem that previously lived as
`coily dispatch`; the operator CLI no longer carries it.

The subsystem itself - ref resolution, the four surfaces, the worktree reaper,
the sidequest registry - lives in the reusable
[`cli-guard/cli/dispatch`](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard)
package. `ward` supplies only the host-specific seams (see
[`cmd/ward/dispatch.go`](../cmd/ward/dispatch.go) and
[`cmd/ward/forgejo_issue.go`](../cmd/ward/forgejo_issue.go)).

## Surfaces

`ward dispatch` requires an explicit surface; bare `ward dispatch <ref>` errors.

- **`ward dispatch headless <ref>`** - spawn a detached `claude -p` in a
  per-issue worktree+branch, log to a file, return immediately. AFK queue work;
  never consults. Lands by merging its branch into main when green.
- **`ward dispatch interactive <ref>`** - open a new tab cwd'd into the
  canonical checkout (default branch, no worktree) with claude pre-submitted
  "Work on issue `<ref>`". Auto mode; the operator may watch but is not consulted.
- **`ward dispatch consult <ref>`** - interactive with a raised interruption
  budget: the agent is encouraged to surface real judgment calls and wait. A
  soft expectation, not plan mode.
- **`ward dispatch cascade <ref>`** - headless, but the worker may recursively
  dispatch sub-workers to split a too-large task. Bounded by a hard depth budget
  (`--depth`, default 3, max 5).

Maintenance verbs: `ward dispatch reap` (remove merged dispatch worktrees),
`ward dispatch status` (pid + log tail for a headless dispatch), and
`ward dispatch registry` (list active sidequests whose pid is still alive).

## Seams ward supplies

- **Allowed owner.** `coilysiren`, plus the primary-org set
  (`coilysiren`, `coilyco-bridge`, `coilyco-flight-deck`, `coilyco-gaming`) via
  `primaryOrgs()`. Refs outside the set are refused.
- **Workspace layout.** `localRepoPath` scans `~/projects/<org>/<repo>` across
  every primary org and falls back to `~/projects/coilysiren/<repo>`. Worktrees
  land under `~/projects/coilysiren/.dispatch-worktrees`, headless logs under
  `~/projects/coilysiren/.dispatch-logs` - both outside any repo.
- **Issue resolution.** A minimal read-only Forgejo client
  (`forgejo.coilysiren.me`, bearer token from SSM `/forgejo/coilyco-ops/api-token`)
  resolves Forgejo refs; a 404 falls back to GitHub via the dispatch package's
  default `gh` resolver for shortform refs.
- **Bot attribution.** The host-side bearer authenticates as the `coilyco-ops`
  bot, not the operator's PAT (ward#160, after #151's ward-kdl move). The token
  (SSM `/forgejo/coilyco-ops/api-token`, minted+rotated by
  `provision-coilyco-ops-bot.sh`) feeds `forgejoAPIToken()` - the bearer for the
  agent issue-verb and `ward dispatch` issue-filing - so automated issue
  create/comment attributes to the bot. The container git-push token
  (`container.go`) stays personal: it needs collaborator push, tracked
  separately.

## `.ward` namespace

ward calls `config.SetAppDir(".ward")` at startup, so every per-user path is
ward's own: audit rows land in `~/.ward/audit/<slug>.jsonl`, session-profile
sentinels under `~/.ward/audit/sessions/<sid>`, and `ward dispatch interactive`
writes its FIFO queue to `/tmp/ward-dispatch-queue` (derived from
`config.BaseName` -> `ward`). The agentic-os Warp launch shim
(`claude-dispatch-interactive`) reads that same queue path; it was repointed
from the retired `coily dispatch`'s `/tmp/coily-dispatch-queue`.

## Audit

Every dispatch runs through cli-guard's verb pipeline: argv is validated against
the shell-metacharacter policy and one JSONL audit row is appended to
`~/.ward/audit/<slug>.jsonl`, same as every other `ward` verb.

## See also

- [docs/FEATURES.md](FEATURES.md) - command inventory.
- [docs/audit.md](audit.md) - the audit log read surface.
