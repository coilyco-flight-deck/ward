# ward docker exec

`ward docker exec` is the **one mutating verb** on the otherwise read-only
[`ward docker`](ward-kdl/ward-kdl.docker.guardfile.md) surface. It shells into a
**ward-managed container** - one ward itself launched via [`ward agent`](agent.md) -
and refuses every other container.

```bash
ward docker exec -it engineer-claude-ward-220 bash   # interactive shell in a run
ward docker exec engineer-claude-ward-220 cat /workspace/ward/AGENTS.md
ward docker exec -u root engineer-claude-ward-220 sh -c 'ps aux | head'
```

The grammar is `docker exec`'s own, unchanged: `[OPTIONS] CONTAINER COMMAND
[ARG...]`, the same `-it` / `-e` / `-u` / `-w` options, forwarded verbatim with
stdio wired so an interactive `-it` shell works.

## The gate: ward=true only

Before forwarding anything, `ward docker exec` runs `docker inspect` on CONTAINER
and confirms it carries the **`ward=true`** label - the marker ward stamps on every
container it runs ([container.md](container.md)). If the label is absent (a
production container, a sibling service, another user's box) the call is refused
before any exec:

```
ward docker exec: refusing to exec into "some-prod-db" - not a ward-managed
container (no ward=true label). ... use bare `docker exec` for anything else
```

Because the target is a runtime value checked against a live label, this cannot be
a pure guardfile grant like the read-only verbs are. It ships as a hand-written
leaf ([cmd/ward/docker_exec.go](../cmd/ward/docker_exec.go)) grafted onto the
guardfile-built `docker` group after mount.

## What exec bypasses relative to the container-permissions posture

The read-only `ward docker` guardfile exposes **only** inspection verbs and leaves
every mutating one to the host lockdown's bare-`docker` deny. `exec` is the
sharpest of those - a shell inside a container is arbitrary code execution there -
so what it does and does not open up is worth stating precisely:

- **Bypasses the read-only posture** of the `ward docker` surface: it is the one
  verb there that changes state (inside the container).
- **Bypasses the shell-metacharacter gate** (`SkipPolicy`). The argv after
  CONTAINER runs *inside* the container via docker's `execve`, never a host shell,
  so metacharacters cannot be reinterpreted host-side. The audit row still records
  the full argv.
- **Does NOT bypass the warded boundary.** The `ward=true` check confines exec to
  boxes ward launched - ephemeral, throwaway [feature containers](container.md)
  already running their agent under `bypassPermissions`
  ([container-permissions.md](container-permissions.md)). You land in a disposable
  clone, not on the host and not in any long-lived service, so exec grants no
  access the box did not already grant itself.
- **Does NOT bypass the audit log.** Every call writes one JSONL row (verb
  `docker.exec`, full argv), like every ward verb.

So [ward#220](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/220)
resolves cleanly: a container-shell, but only into boxes already fully autonomous
and short-lived, every use recorded. For any container ward did not launch it is a
hard refusal - reach for bare `docker exec` there, where the host lockdown decides.

## See also

- [ward-kdl/ward-kdl.docker.guardfile.md](ward-kdl/ward-kdl.docker.guardfile.md) - the read-only docker verbs this sits beside.
- [container.md](container.md) - the ward-managed container model and its `ward.*` labels.
- [container-permissions.md](container-permissions.md) - the `bypassPermissions` posture inside a run.
- [agent.md](agent.md) - `ward agent`, the launcher that creates exec's targets.
