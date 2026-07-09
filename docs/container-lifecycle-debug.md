---
doc_goal: Give an operator a self-contained procedure to reconstruct any headless warded run's path from its logs alone - the three terminal reap states, how to tell a bootstrap death from a never-launched dispatch, and the durable-surfaces-outward sequence - so a run is debuggable with no prior private context.
---
# debug a headless run from logs alone

The companion to [container-lifecycle-logs.md](container-lifecycle-logs.md) (the log
conventions): how to reconstruct a warded run's path from its logs with no prior
private context
([ward#517](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/517)).

## Reading a run's shape from its terminals

A run ends in exactly one of three terminal states, each a distinct reap line:

- **landed** - `WARD-REAP: nothing to reap ...` or `ward container reap: landed on main`. Clean success.
- **salvaged** - `ward container reap: salvage start ...` then `preserved work on
  ward-salvage/<id>`. The [reap diagnostics block](container-reap-diagnostics.md)
  (`--- reap diagnostics ---`) prints alongside and self-explains which gate fired.
- **reopened** - `WARD-REAP: ...` plus `posted salvage notice to carried issue #N` / `reopened #N`. A
  salvage that had a carried issue reopens it, undoing any premature `closes #N`.

A container that never reached reap died in bootstrap: look for `ward-container: ...
fatal:` or `bootstrap prelaunch check failed` with no later `bootstrap launch
handoff`. A run that produced **no container at all** was stopped at dispatch - its
reason is a host-terminal `ward agent` line (`pre-flight NO-GO`, `WRONG-REPO`, or a
reservation conflict), not anything in `console.log`.

## The recommended sequence

From the durable surfaces outward:

1. **Classify it from `meta.json`.** In `~/.ward/agent-logs/<container>/meta.json`
   the `outcome` field already names the end state. The directory name is the
   `container` correlation id you carry onward. Sink selection:
   [agent-observability.md](agent-observability.md).
2. **Pull the whole run.** `grep container=<name>` the `console.log` to gather
   every in-container line for that run in one view.
3. **Walk bootstrap.** Confirm the run reached `clone done`, `provenance recorded`,
   hook install, compose, and `bootstrap prelaunch check passed` to `bootstrap
   launch handoff`. The first stage logging `start` with no matching `done`/`passed`
   is the failure point. A `prelaunch check failed` is almost always a bad seeded
   credential ([agent-credentials.md](agent-credentials.md)).
4. **Walk reap.** From `reap: start`, follow the `decision=` gate to one of the three
   terminals above. On a salvage, read the adjacent
   [reap diagnostics block](container-reap-diagnostics.md): ward version, the
   HEAD-vs-`origin/main` ancestry verdict, and the exact gate that tripped.
5. **Only if no container appeared, read the dispatch terminal.** The `ward agent`
   lines carry the pre-flight verdict and reservation outcome that blocked the
   launch. ward also posts a NO-GO or salvage comment to the issue, so the issue
   thread is a durable stand-in when terminal scrollback is gone.

## See also

- [container-lifecycle-logs.md](container-lifecycle-logs.md) - the log surfaces, grammar, and correlation ids this flow leans on.
- [troubleshooting.md](troubleshooting.md) - symptom-indexed entry into the same surfaces.
- [container-reap.md](container-reap.md) - the reaper whose decisions these lines narrate.
- [agent-dispatch-contract.md](agent-dispatch-contract.md) - the `meta.json` outcome enum a supervising run reads.
