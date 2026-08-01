---
doc_goal: Give the dev-verb gate a compact release-era reference so contributors know what `ward exec` does and what it refuses.
---
# ward exec

`ward exec <verb>` runs a repo dev verb through the gate.

## The contract

- argv is validated before the verb runs.
- one audit row is written for every invocation.
- a named branch must retain the existing clean-and-synced contract.
- a detached HEAD is admitted only for a clean Forgejo Actions pull-request
  merge checkout whose environment, event payload, origin, workspace, HEAD,
  and two merge parents agree.
- unknown verbs fall back to `ward exec` routing.

The detached path requires `FORGEJO_ACTIONS`, `GITHUB_ACTIONS`, and `CI` to be
true. Ward matches the repository and server to `origin`, verifies
`GITHUB_WORKSPACE` is the discovered repository, matches `GITHUB_SHA` to the
immutable detached commit, and matches the event base and head SHAs to that
commit's two parents. Missing or inconsistent evidence fails closed. A local
detached checkout remains refused, and `--audit-override-dirty` does not bypass
this path.

An accepted detached invocation records a typed `ci` object in the audit row.
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
- [verb-fallback.md](verb-fallback.md) - the fallback rule.
