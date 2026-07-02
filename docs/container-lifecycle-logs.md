# container lifecycle logs

[ward#516](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/516) made
every discrete step of a warded run emit a stderr marker. This page is the
**discipline layer** on top
([ward#517](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/517)): the
stable structure those markers follow. The debug-from-logs-only sequence lives in
[container-lifecycle-debug.md](container-lifecycle-debug.md).

## The three log surfaces

A run's markers come from three emitters, each with a fixed grep prefix. Two run
**inside the container**, so `docker logs` captures them into the drained
`console.log`. The first runs on the **host** at dispatch and reaches only the
operator's terminal.

- **`ward agent`** - **dispatch (host)** - ref resolution, reservation, the GO/NO-GO
  pre-flight, launch handoff. Lines start `ward agent:` or `ward agent <role>
  --driver <mode>:` (grep the stem `ward agent`, not the colon). These run before the
  container exists, so they are **not** in `console.log`.
- **`ward-container:`** - **bootstrap (in container)** - the PID-1 entrypoint: clone,
  provenance, hook install, extra-repo clone, substrate warm, compose, pre-launch
  check, launch handoff. Note the **hyphen** (`ward container:` will not match).
- **`ward container reap:`** - **reap (in container)** - teardown: residual commit,
  integrate, the land-or-salvage decision, push, salvage, reopen, grant verification.

The drained `console.log` is `bootstrap + agent transcript + reap`, in order.

## The line grammar

Every marker follows one shape:

```
<surface>: <stage> <event> <key>=<value> ... [- <human note>]
```

- **`<stage> <event>`** names the step. The event word is a small fixed set. `start`
  began. `done`/`ready`/`clean`/`acquired`/`passed`/`landed` succeeded.
  `failed`/`rejected`/`missing` degraded. `skipped`/`keep` are deliberate no-ops. A
  stage logging `start` with no later `done`/`failed` is where the run died.
- **`<key>=<value>`** fields carry the facts, never free prose. Stable keys:
  `container`, `issue`, `repo`, `branch`, `mode`, `driver`, `agent`, `readOnly`,
  `headless`, `extraRepos`, `force`, `verdict`, `reason`, `baseline`, `decision`.
- **`- <human note>`** trails degraded lines with the reason or recovery hint.

## Correlation identifiers

Three keys stitch one run across the host terminal, `console.log`, `meta.json`, and
a salvage issue:

- **`container=<name>`** - **the run id.** It is `WARD_CONTAINER_NAME`, and the same
  value appears in the dispatch `launch plan ready` line, the bootstrap `start`
  line, the reap `start` and `salvage start` lines, the `~/.ward/agent-logs/<name>/`
  directory name, and `meta.json`'s `container` field. One `grep container=<name>`
  over `console.log` returns a run's in-container lifecycle.
- **`issue=<N>` / `#<N>`** - the carried issue, spanning every surface and the
  salvage reopen it writes back to.
- **`repo=<owner>/<name>`** - the target repo on a multi-repo host.

The salvage branch (`ward-salvage/<repo>-<hex>`) is stemmed on the repo name, not
the container, so the reap `salvage start` line's `container=` field ties a
preserved branch back to its run.

## No secrets on this surface

The markers carry only non-secret dims - container, issue, repo, branch,
mode/driver, flags, counts, git shas, verdict and decision codes. Tokens and
credentials (`FORGEJO_TOKEN`, `WARD_CLAUDE_CREDS_B64`) are never formatted into a
line, so `console.log` can ship to a SigNoz sink and the
[reap diagnostics block](container-reap-diagnostics.md) can fold into a public
issue. Keep new lines to those `key=value` dims.

## See also

- [container-lifecycle-debug.md](container-lifecycle-debug.md) - the debug-a-headless-run-from-logs-only sequence.
- [agent-observability.md](agent-observability.md) - where these logs drain, the sink modes, the locality gate.
- [container-reap-diagnostics.md](container-reap-diagnostics.md) - the salvage/failure self-diagnosis block.
