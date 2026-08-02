---
doc_goal: Explain repository-independent collaboration cluster launch, lookup, logs, and isolated cleanup.
---
# collaboration clusters

A collaboration cluster is a supervised broker plus any director or peers that
attach later. Start an empty cluster from any directory, including one that is
not a Git checkout:

```bash
ward agent cluster start --harness codex
# codex-ab45
```

The start path creates only the broker service. It does not resolve a
repository, select a repository workflow, project Forgejo credentials, or
launch a director. Docker supervises the broker with `restart: unless-stopped`.

The returned cluster id is the only required lifecycle key:

```bash
ward agent cluster list
ward agent cluster status codex-ab45
ward agent cluster logs codex-ab45
ward agent cluster stop codex-ab45 --print
ward agent cluster stop codex-ab45
```

`cluster status` is also the live peer roster. It shows the broker-minted peer
id, role, harness, status, and container without consulting a repository.

Status, logs, and stop filter on the exact `ward.cluster` label. Stop removes
only containers carrying that id, then removes that Compose project and its
Ward-owned state directory. Repository metadata may still appear on an
explicit repository-backed peer, but it never participates in cluster lookup.

## Bounded Docker Desktop check

After `cluster start`, Docker Desktop is expected to show one Compose
application named exactly like the returned cluster id with one `broker`
service. A director-backed cluster shows broker and director under that same
application. This is a convenience check only. The Compose project name,
`ward.cluster` labels, and automated lifecycle isolation tests are the
authority contract.

## See also

- [agent-dispatch-broker.md](agent-dispatch-broker.md) - broker protocol and restart contract.
- [agent-peer-collaboration.md](agent-peer-collaboration.md) - attaching peers and exchanging messages.
- [terminology.md](terminology.md) - cluster, run, role, and harness distinctions.
