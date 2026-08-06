---
doc_goal: Define Ward's serialized, recoverable deploy-state transaction without embedding provider behavior or authority.
---
# Agent release transaction

`ward agent release execute` turns one broker-minted candidate and attempt into
one durable environment outcome. Ward owns ordering and recovery. Existing
AOSguard operations own provider authorization, deployment, and observation.

## Inputs

Ops supplies the candidate ID, attempt ID, deploy-repository worktree, and one
prepared commit:

```bash
ward agent release execute --candidate <id> --attempt <id> \
  --worktree <path> --prepared-commit <full-commit>
```

The local commit may be prepared before execution. Ward admits it only after
locking the environment, refetching the branch, and verifying the starting
state. It must be one nonempty child of `starting_deploy_commit` in a clean
worktree whose raw origin names the candidate's canonical Forgejo repository.
Its message must contain exact trailers for application revision, artifact
digest, environment, Ward run, originating ticket, and candidate ID.

## Transaction

1. Journal acceptance and create a deterministic remote Git lock ref keyed by
   deploy repository, branch, and environment.
2. Refetch the deploy branch. Reject a stale starting commit before mutation.
3. Run the named verify operation and require a typed attestation for the
   current environment and starting deploy commit.
4. Validate the prepared commit, journal mutation intent, and run the named
   deploy operation for that exact desired state.
5. Verify the application commit, artifact digest, and prepared deploy commit.
6. Push the prepared commit with an exact starting-revision lease. Reread the
   branch after every push response before classifying the outcome.

The lock ref is transient. Ward creates and deletes it with exact object-ID
compare-and-swap. Another attempt is `blocked`. Ward never steals a lock by age
or deletes a ref whose ownership changed. The deploy branch is the only durable
deploy-state truth.

## Failure and restart

Failure after mutation makes Ward reapply the preflight state and verify it.
That produces `restored` only when the typed attestation matches. An unreadable
or divergent environment, branch, push, or restore produces `indeterminate`
and retains the lock for reconciliation.

Ward journals every phase in `release.jsonl`. Repeating an identical phase or
terminal result is idempotent. A restart resumes its own lock, reconciles the
remote branch and environment, and either completes the verified push or
restores the starting state. A failed lock deletion records `cleanup-needed`.

## Provider boundary

Candidate operations are symbolic `area.verb` IDs. Ward invokes `aosguard ops
<area> <verb>` with validated `WARD_RELEASE_*` identifiers. The candidate does
not grant that operation. AOSguard independently authorizes it.

Deploy output becomes only a bounded evidence digest. Verify output must be one
strict JSON object containing full application and deploy commits, artifact and
evidence SHA-256 digests, and no unknown fields. Raw output never enters the
journal or result.

Rollback is a new candidate from the current deploy commit to a previous
immutable application revision. It creates and verifies a new child commit.
Ward never rewinds or force-replaces deploy history.

## See also

* [agent-release.md](agent-release.md) - candidate, result, and retry contract.
* [agent-ops.md](agent-ops.md) - operator surfaces and retained state.
* [agent-observability.md](agent-observability.md) - artifact boundaries.
