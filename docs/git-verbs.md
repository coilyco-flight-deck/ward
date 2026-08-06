---
doc_goal: Define Ward's governed Git verbs, audit behavior, auth wiring, concurrency-safe commit, and destination-gated clone.
---
# `ward git`

`ward git` exposes supported Git operations through Ward's audit wrapper. It
preserves configured network auth, reports the underlying Git exit status, and
records the invocation under the same repository audit trail as `ward exec`.

Common forms are:

```bash
ward git status
ward git commit -m "message"
ward git fetch origin
ward git push origin main
ward git clone <url> [directory]
```

Commit uses Ward's concurrency-safe commit path. Network verbs use the
resolved forge credential channel without writing tokens to argv or audit.

## Clone gate

`ward git clone` admits a clone when either:

* The absolute, symlink-canonicalized destination is under `/tmp`, the platform
  temporary directory, or `$TMPDIR`. Any repository may use this disposable path.
* The parsed `owner/repository` is in Ward's compiled persistent-clone allowlist.

Ward resolves an explicit destination relative to cwd, or relative to a
leading `-C <directory>`. Without a destination, it derives Git's normal
repository basename under that base directory. It then applies the gate before
invoking Git with the original clone arguments.

An off-allowlist repository belongs in an ephemeral directory. A repository
that legitimately belongs on persistent disk requires a product change to the
compiled allowlist. Repository or operator YAML cannot expand it.

## See also

* [exec-verb.md](exec-verb.md) - repository command gate.
* [audit.md](audit.md) - audit records.
* [config-source.md](config-source.md) - config and credential ownership.
