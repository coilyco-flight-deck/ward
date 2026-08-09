---
doc_goal: Give the dev-verb gate a compact release-era reference so contributors know what `ward exec` does and what it refuses.
---
# ward exec

`ward exec <verb>` runs a repo dev verb through the gate.

## The contract

- argv is validated before the verb runs.
- one audit row is written for every invocation.
- the gate refuses exactly one thing: an uncommitted `.ward/ward.yaml`. That is
  the file the verb argv is read from, so a row naming a verb whose definition
  is not in git history cannot be followed up later.
- `--audit-override-dirty` bypasses that refusal and tags the row
  `audit_override=true` with the working-tree status attached.
- a declared command typed as `ward <verb>` falls back to `ward exec <verb>`
  only when `<verb>` is not a registered top-level command.

Nothing else blocks a verb, and the gate contacts no remote. Dirt elsewhere in
the tree, a detached HEAD, a branch with no upstream, and a branch behind its
upstream are all recorded on the audit row and all run. The row already stores
the resolved `argv` verbatim, so the committed config is what lets a reader
find the verb definition later, not what proves what ran. This replaces an
earlier clean-and-synced contract whose `git fetch` cost a network round trip
on every invocation and refused offline.

## CI attribution

Forgejo Actions attribution is evidence, not a gate. When `FORGEJO_ACTIONS`,
`GITHUB_ACTIONS`, and `CI` are all true and the event is a pull request, ward
matches the repository and server to `origin`, verifies `GITHUB_WORKSPACE` is
the discovered repository, matches `GITHUB_SHA` to the checkout commit, and
matches the event base and head SHAs to that commit's two merge parents.

Missing or inconsistent evidence leaves the row **unattributed**. It does not
refuse the verb. A row that silently claimed the wrong pull request would be
worse than one that claims nothing.

An accepted Forgejo Actions invocation records a typed `ci` object in the audit row.
It includes the provider server and repository, event ref, pull-request number,
base and head refs, immutable HEAD SHA, workflow, job, actor, run ID, run number,
and run attempt.

## Enforcement depth

- on Linux, the gate can run inside the sandbox jail.
- on macOS and Windows, enforcement is shallower and follows the host
  allowlist.
- the Linux jail is not a privilege-escalation path. `sudo` stays blocked by
  `no_new_privs`, so `become:true`-style converges cannot self-elevate inside
  the jail. `ask_pass` does not change that. Jailed SSH can also reject
  root-owned config includes under userns mappings.

The important part is that the verb still passes through the same audited path
either way.

## What it is for

- `build`, `test`, `lint`, `tidy`, `cover`, and similar repo verbs.
- the `ward git` surface and the audit trail around it.
- privileged converge steps that can run without needing `sudo` or jail-local
  SSH config ownership checks.

## Example

```bash
ward exec test
ward exec lint
ward exec build
```

The command you type is not a free shell. It is a checked, logged repo verb.

## See also

- [ward-yaml.md](ward-yaml.md) - the repo config schema.
- [audit.md](audit.md) - the audit log format.
- [git-verbs.md](git-verbs.md) - governed Git operations.
