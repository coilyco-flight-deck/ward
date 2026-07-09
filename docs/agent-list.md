---
doc_goal: Let a director operating a read-only surface list the currently running engineer containers through the broker, using ward labels and reservation data where available, with a stable human table and `--json` machine output that makes it easy to answer which engineer is carrying an issue.
---
# ward agent list: listing running engineers

`ward agent list` prints the running engineer containers that ward knows about.
It is the read-only sibling to [`ward agent stop --print`](agent-stop.md): both
use the same label-backed target resolution, but `list` reports the whole active
set instead of a single target.

## How it runs

- From a director read-only surface, `list` forwards through the same broker
  path as `stop` and `logs`.
- Off the surface, it reads the local Docker state directly.
- It uses ward labels first, then reservation data when present, so the output
  stays issue-aware instead of guessing from container names alone.

## Fields

Each running engineer row can include:

- container name
- harness
- repo
- issue number and ref
- branch
- host
- reserved-at
- started-at
- age
- status

## Output

- Default - a stable human table.
- `--json` - a stable machine-readable schema.
- `ps` - alias for `list`.

## Usage

```bash
ward agent list
ward agent ps
ward agent list --json
```

## See also

- [agent-stop.md](agent-stop.md) - the same broker path, one target at a time.
- [agent-logs.md](agent-logs.md) - the same broker path for logs instead of inventory.
- [agent-surface.md](agent-surface.md) - the director surface this command runs from.
- [FEATURES.md](FEATURES.md) - inventory.
