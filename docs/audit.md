# audit log

ward writes one append-only JSONL row per audited invocation (every
`ward exec` repo verb and `ward pkg brew` run) to a per-repo file:

```
~/.coily/audit/<slug>.jsonl
```

The `<slug>` is derived from the repo's origin remote (via
cli-guard/config), so each repo gets its own log. ward and coily share
the same slug scheme, so a repo's `coily` and `ward` rows land in one
file.

## Reading the log

`ward audit path` prints the resolved file path for the current repo:

```
$ ward audit path
/Users/you/.coily/audit/coilyco-flight-deck-ward.jsonl
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

Rows are cli-guard `audit.Record` values: `ts`, `verb` (`repo.<cmd>` for
exec verbs), `argv`, `decision`, `exit_code`, `repo_root`, and - on
dirty-tree overrides - `audit_override` plus `working_tree_status`. See
[exec-verb.md](exec-verb.md) for the gate that stamps the last two.
