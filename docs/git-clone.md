# ward git clone (destination-gated)

`ward git clone <url> [dir]` wraps `git clone` behind a destination gate
so an agent cannot drop an unwanted **persistent** checkout into the
tracked workspace (ward#285). It is registered alongside the other git
verbs in `cmd/ward/git.go`; see [git-verbs.md](git-verbs.md).

## Why

An agent cloned a repo into `~/projects` against the operator's intent.
The harm is **where the clone lands** (a persistent path), not which repo.
So the gate is on the resolved destination, with a hardcoded repo
allowlist as the escape hatch for clones that legitimately live on disk.

## Rule

A clone is allowed iff EITHER:

- the **resolved destination** is under an ephemeral root (`/tmp` or
  `$TMPDIR`) - any repo, since the checkout is throwaway; OR
- the **repo** (owner/name parsed from the URL) is on ward's **hardcoded
  allowlist** - then a persistent destination is fine.

Otherwise (off-allowlist repo into a non-ephemeral destination): refused.

## How

- **cwd-aware destination.** The effective destination is the explicit
  `[dir]` if given, else `cwd/<basename-of-url>` (bare `git clone <url>`
  lands in cwd). It is resolved to an absolute, symlink-canonicalized path
  before the gate runs - which is exactly why this is a Go verb and not a
  guardfile glob (the exec dialect's matcher only sees argv tokens, never
  cwd). A leading `-C <dir>` selects the base directory.
- **Hardcoded, tamper-resistant allowlist.** The allowlist is baked into
  the binary (`cmd/ward/git_clone.go`), curated to the fleet's on-disk
  intent (cf. `agentic-os-kai scripts/repos-on-disk.txt`), not the broader
  substrate preclone set (ward#290). ward itself and cli-guard are
  deliberately off it - agents that need either clone into `/tmp`
  (ephemeral), which the gate already allows. ward never reads the
  allowlist from an agent-writable file, so an agent cannot widen its own
  escape hatch. To add a repo that belongs persistently, edit the
  allowlist in source and ship a new build.

## Enforcement

Raw `git clone` is denied in the agent lockdown
(`cmd/ward/containerassets/settings.container.json`), forcing every agent
clone through `ward git clone`. The operator's interactive shell and
ward's internal Go bootstrap (which execs git directly, not via the Bash
tool) are unaffected.
