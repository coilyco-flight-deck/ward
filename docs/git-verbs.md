# git verbs

`ward git` fronts the contributor git surface behind cli-guard's audit +
argv-validation pipeline. Every invocation validates argv and appends one
audit row (`git.<verb>`) to the per-repo log. Ported from coily's `git`
group (coily#7).

## Passthroughs

These are thin audited passthroughs to the underlying `git <verb>`:

```
ward git status | log | diff | show | add
ward git fetch | pull | push
ward git branch | checkout | stash | restore
ward git remote
```

`remote` is a read passthrough for resolving repo identity, e.g. `ward
git remote get-url origin` (and plain `ward git remote` to list remotes),
mirroring bare `git remote ...` behind the audit pipeline.

A leading `-C <dir>` is hoisted ahead of the subcommand, so `ward git
status -C /path` runs `git -C /path status` (lets a session operate on a
repo other than cwd).

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
