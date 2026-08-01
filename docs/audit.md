---
doc_goal: Keep the audit log reference short and stable so it explains the immutable trail without dragging in launch or container details.
---
# ward audit

`ward audit` reads the append-only JSONL trail written by gated verbs.

- `path` prints the log path.
- `tail` streams rows, optionally with `--follow` and `--since`.

The trail is per-repo and records one row per guarded invocation.

## Why it exists

- it makes the run reconstructable from git history.
- it gives troubleshooting a stable first artifact.
- it shows what actually ran, not what the user thought they typed.

## What to look for

- the repo name.
- the verb or command.
- the timestamp.
- the final result.
- for an accepted detached Forgejo Actions checkout, the typed `ci` object with
  repository, pull-request, commit, workflow, actor, and run attribution.

## See also

- [exec-verb.md](exec-verb.md) - the verbs that write the trail.
- [troubleshooting.md](troubleshooting.md) - what to read when a run fails.
