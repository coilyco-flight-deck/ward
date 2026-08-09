---
doc_goal: Keep the audit log reference short and stable so it explains the immutable trail without dragging in launch or container details.
---
# ward audit

`ward audit` reads the append-only JSONL trail written by gated verbs.

- `path` prints the log path.
- `tail` streams rows, optionally with `--follow` and `--since`.

The trail is per-repo and records one row per guarded invocation.

## Why it exists

- it shows what actually ran, not what the user thought they typed. The row
  stores the resolved `argv` verbatim, so it is the record, not a pointer to one.
- it gives troubleshooting a stable first artifact.
- the committed `.ward/ward.yaml` behind it lets a reader find the verb
  definition in git history when the argv alone is not enough context.

## What to look for

- the repo name.
- the verb or command.
- the timestamp.
- the final result.
- for a Forgejo Actions checkout with valid evidence, the typed `ci` object with
  repository, pull-request, commit, workflow, actor, and run attribution. It is
  absent when the evidence was incomplete or inconsistent, which never blocks
  the run - an absent `ci` means unproven, not failed.

## See also

- [exec-verb.md](exec-verb.md) - the verbs that write the trail.
- [troubleshooting.md](troubleshooting.md) - what to read when a run fails.
