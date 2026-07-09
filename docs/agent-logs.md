---
doc_goal: Explain `ward agent logs` as the brokered read path for one engineer run's logs - live Docker first, drained archive fallback, source reported, tail/follow aware - so a director surface can retrieve logs without a socket or a bind mount.
---
# ward agent logs: reading one engineer run

`ward agent logs <ref>` reads one engineer run's logs and prints the source it used
before the body streams. It is the direct counterpart to `ward agent stop`: the
same target shapes, the same director-surface broker path, but read-only.

## How it resolves

The command accepts an issue ref or a container name.

- If the engineer container is still present, it prefers `docker logs`.
- If the container has been removed, it falls back to the drained archive at
  `~/.ward/agent-logs/<container>/console.log`, or the redacted sibling when
  that is what remains.
- If the live Docker lookup is ambiguous, it fails loud instead of guessing.

## Flags

- `--tail N` - show the last `N` lines.
- `--follow` - stream live Docker logs. This only works when the source is live.

## Usage

```bash
ward agent logs coilyco-flight-deck/ward#692
ward agent logs engineer-goose-ward-692
ward agent logs coilyco-flight-deck/ward#692 --tail 200
ward agent logs coilyco-flight-deck/ward#692 --follow
```

## See also

- [agent-surface-log-read.md](agent-surface-log-read.md) - the mounted read-only surface for the same archive tree.
- [agent-stop.md](agent-stop.md) - the sister brokered control verb.
- [FEATURES.md](FEATURES.md) - inventory.
