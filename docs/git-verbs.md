# git verbs

`ward git` fronts the contributor git surface behind cli-guard's audit +
argv-validation pipeline. Every invocation validates argv and appends one
audit row (`git.<verb>`) to the per-repo log. Ported from coily's `git`
group (coily#7).

## Passthroughs

These are thin audited passthroughs to the underlying `git <verb>`:

```
ward git status | log | diff | show | grep | add
ward git fetch | pull | push
ward git branch | checkout | stash | restore
ward git remote
```

`grep` is a read-only content search over the current clone (flags pass to
`git grep`). `remote` resolves repo identity, e.g. `ward git remote
get-url origin` (plain `ward git remote` lists remotes), mirroring bare
`git remote ...` behind the audit pipeline.

A leading `-C <dir>` is hoisted ahead of the subcommand: `ward git status
-C /path` runs `git -C /path status` (operate on a repo other than cwd).

## clone (destination-gated)

`clone` is not a passthrough. It wraps `git clone` behind a destination
gate so an agent cannot drop an unwanted **persistent** checkout into the
tracked workspace. A clone is allowed iff its resolved destination is
under an ephemeral root (`/tmp`/`$TMPDIR`) OR the repo is on a hardcoded
allowlist. See [docs/git-clone.md](git-clone.md) for the full walkthrough.

```
ward git clone <url> [dir]
```

## grep-remote (ephemeral-clone code search)

Forgejo has no REST code-search, so `ward git grep-remote <owner/repo>
<pattern> [flags]` shallow-clones the repo (`--depth 1`) into an ephemeral
temp dir via the `ward git clone` gate, greps tracked files at HEAD, then
removes it. Args after the repo forward to `git grep`; scope is clone-local
(no cross-repo or server-side search). A no-match grep (exit 1) is empty.

## commit (concurrency-safe)

`commit` is not a passthrough. It is a dedicated verb that is safe when
multiple agent sessions share one working tree. It requires:

```
ward git commit -m "msg" -- <path> [<path>...]
```

### Why

Two sessions sharing one checkout run `add` then `commit` as separate
invocations. A second session's `add`/`commit` can interleave in the gap,
and because `.git/index` and `.git/COMMIT_EDITMSG` are shared process-
global files, one session's content lands under the other's message.

### How

- **Explicit pathspecs.** The `--` separator and named paths commit the
  worktree content of exactly those paths (seeded from HEAD), so another
  session's staged files cannot leak in.
- **Private index.** `GIT_INDEX_FILE` points at a throwaway index seeded
  from HEAD, so the commit never reads or writes the shared `.git/index`.
  Seeding from HEAD plus `git add -A -- <paths>` lets new, modified, and
  deleted paths all commit uniformly (the empty-index case used to refuse
  new files).
- **No editor.** The message must come from `-m`/`-F`; `-e`/`--edit` is
  refused, so `.git/COMMIT_EDITMSG` is written-not-read and messages
  cannot cross.

After the commit, the named paths are resynced in the shared index to the
new HEAD (`git reset -q HEAD -- <paths>`, best-effort) so the next `git
status` reads clean for them without disturbing another session's staged
entries.
