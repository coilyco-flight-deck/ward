---
doc_goal: Keep the logs-anchor stable after the old page was collapsed.
---
# agent logs

This page is the durable anchor for reading one run's logs.

- It prefers the live container and falls back to the drained archive.
- It is the first read when a run looks stuck or silent.
- It is an operator surface, not a repo policy surface.
- Brokered dispatch failures also land here as first-class artifacts under
  `~/.ward/agent-logs-redacted/dispatch/`, and `ward agent logs` can resolve the newest
  dispatch artifact by issue/ref or by role.

## Artifact selector

`ward agent logs <target>` defaults to the console artifact. Add
`--artifact <name>` to read a specific completed-run artifact:

```bash
ward agent logs critic-ab45
```

A broker-minted peer id resolves through the `ward.peer` label. Inside a
cluster, lookup is scoped to that cluster. Host lookup refuses duplicate ids
instead of choosing a container. Container names and issue refs retain their
existing behavior.

- `console` - live `docker logs` while the container is running; for an exited
  container with a completed archive, the drained console is preferred so the
  `WARD-RUN-SUMMARY` footer is visible.
- `transcript` - the drained secret-safe tool-envelope artifact. If metadata says a transcript existed but
  no transcript artifact is readable, Ward returns a secret-free JSON summary
  for the missing artifact instead.
- `meta` - the drained secret-free `meta.json`.
- `friction` - a schema-versioned JSON report with run correlation fields and
  an explicit `events` array. Clean runs return `"events": []`; recovered
  broker friction and terminal launch failures are classified as events.
- `dispatch` - the secret-safe broker wrapper artifact for the issue/ref. This
  selector intentionally chooses the dispatch artifact even when a matching
  engineer container is live or exited.

Every reader receives the same secret-safe artifact. Ward never falls back to
the retired historical raw archive.

## See also

- [agent-ops.md](agent-ops.md) - logs, stop, list, reap.
- [troubleshooting.md](troubleshooting.md) - symptom-indexed recovery.
