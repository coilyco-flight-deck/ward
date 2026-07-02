# exec verb

The `ward exec` verb walks up from cwd looking for a
ward or coily allowlist, then exposes each declared command
as a leaf subcommand.

Unknown top-level verbs that match a declared leaf fall back to `exec`
automatically (`ward test` -> `ward exec test`), and every ward-managed repo
is expected to declare the `build`/`test`/`install` triple. See
[docs/verb-fallback.md](verb-fallback.md).

When no config is reachable, `exec` is still registered (so `--help`
and the `version` verb behave consistently) but every invocation
returns a clear error pointing at the missing file.

Each leaf runs the configured argv inside the repo that declared it,
after validating every argv token against cli-guard's
shell-metacharacter policy. Every invocation runs through cli-guard's
verb pipeline, so each `ward exec` run appends one JSONL audit row to
`~/.ward/audit/<repo>.jsonl` (verb prefix `repo.<cmd>`).

For a concrete demo of what the gate turns away - the clean-tree refusal and the
argv-metacharacter refusal side by side, with the loud-override note - see
[docs/gate-demo.md](gate-demo.md).

## Enforcement depth by platform

The gate is a **verb-level** boundary - it bounds what call is expressible, not
what a process can touch once running (see
[docs/comparison-openshell.md](comparison-openshell.md)). How deep that boundary
holds against **descendant** processes differs by platform, and the difference is
material:

- **Linux: depth-N.** Sandboxed verbs (e.g. `ward pkg brew`) run inside
  cli-guard's `sandbox` jail, so the wrapper holds at arbitrary process depth,
  not just depth 0. A descendant of a gated verb that invokes a wrapped tool -
  by name or absolute path - re-enters the gate. A descendant escape here is a
  vulnerability ([SECURITY.md](../SECURITY.md)).
- **macOS / Windows: depth-0.** There is no sandbox jail. Enforcement is the
  harness allowlist alone, which only sees the top-level call: a child process
  spawned by a gated verb can invoke a wrapped tool without re-entering the gate.
  This descendant bypass is a **known limitation by design**, not a vulnerability.

So the strongest containment property - the gate holding at arbitrary process
depth - is **Linux-only**. macOS and Windows adopters (the audience the
brew-first [Install](../README.md#install) path predominantly serves) get
top-level argv validation, the audit row, and the clean-tree gate, but **not**
descendant containment. Scope any "the gate holds no matter how deep the call
nests" expectation to Linux.

## Clean-tree gate

A repo verb's audit row records the argv expanded from `.ward/ward.yaml`,
so the row can only be reconstructed from git history if the declaring
`ward.yaml` is committed and HEAD has a synced upstream branch. The gate
(cli-guard's `gittree`) therefore refuses an `exec` verb when:

- the declaring `ward.yaml` is itself among the dirty paths, or
- HEAD is detached / has no upstream / is behind its upstream.

Working-tree dirt that does **not** touch `ward.yaml` does not refuse -
the committed `ward.yaml` plus HEAD still reconstruct the invocation - but
the `git status --porcelain` snapshot is stamped onto the audit row
(`working_tree_status`) for forensics.

### Override

`ward --audit-override-dirty exec <cmd>` bypasses the gate for genuine
emergencies. The audit row is tagged `audit_override=true` and captures
the working-tree status so the run can still be reconstructed after the
fact. This mirrors `coily --audit-override-dirty`.

This is the contributor-side port of coily's repo-verb gate
([coilysiren/coily#211](https://forgejo.coilysiren.me/coilyco-bridge/coily/issues/211)).
