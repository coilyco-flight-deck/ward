---
doc_goal: Give a contributor everything needed to find, stream, scope, and parse ward's per-repo audit log, and convey that the append-only JSONL row is ward's load-bearing proof surface - reconstructable from git history because verbs gate on a clean, synced tree - not an incidental log file.
---
# audit log

The audit row is where ward's boundary-is-the-product thesis becomes
checkable. Each row is the durable proof that an audited run - or a
refusal - actually happened. Because every repo verb is gated on a clean,
synced tree, the row is reconstructable from git history: the commit it
ran against is known, so the record cannot drift from the code it
describes. That gate is not incidental hygiene. It exists so the proof
holds, which is the whole point of writing the row.

ward writes one append-only JSONL row per audited invocation (every
`ward exec` repo verb and the audited host-verb runs) to a per-repo file:

```
~/.ward/audit/<slug>.jsonl
```

The `<slug>` is derived from the repo's origin remote (via
cli-guard/config), so each repo gets its own log. ward and coily share
the same slug scheme, so a repo's `coily` and `ward` rows land in one
file.

## Reading the log

`ward audit path` prints the resolved file path for the current repo:

```
$ ward audit path
/Users/you/.ward/audit/coilyco-flight-deck-ward.jsonl
```

`ward audit tail` streams the rows as JSONL (parse with `jq` or any JSON
library):

```
ward audit tail                     # every row in the file
ward audit tail --since 1h          # rows from the last hour
ward audit tail --since 7d --follow # last week, then block for new rows
```

`--since` accepts unix seconds or a duration (`5m`, `1h`, `24h`, `7d`).
`--follow` replays history then polls for appends.

### Scope filter

`--scope` restricts to rows whose `repo_root` matches a path (the
directory itself or any descendant). The sentinels `.` and `here` resolve
to the current git toplevel (via cli-guard/scope), so a contributor can
narrow a shared slug file to just the rows recorded in this checkout:

```
ward audit tail --scope here        # only this repo's rows
ward audit tail --scope /path/to/repo
```

## Row schema

A row records both the **decision** and the **outcome**, so a reader can
tell an allowed run from a refusal and see the exact argv each was made
against - that is what makes it usable as proof, not just a log line.

Rows are cli-guard `audit.Record` values: `ts`, `verb` (`repo.<cmd>` for
exec verbs), `argv`, `decision`, `exit_code`, `repo_root`, and - on
dirty-tree overrides - `audit_override` plus `working_tree_status`. See
[exec-verb.md](exec-verb.md) for the gate that stamps the last two.
