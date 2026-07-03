# ward agent reap

`ward agent reap` is the **host-side idle-killer** for wedged engineer
containers ([#376](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/376)).
It is distinct from [`ward container reap`](container-reap.md): that one is the
in-container teardown backstop that lands or salvages a run on exit; this one runs
**on the host** and stops a container that is still `Up` but has gone silent.

## Why log-staleness is a sound idle signal

A [`ward agent engineer`](agent-engineer.md) run is fire-and-forget and **exits
cleanly when its work is done**. So an engineer still `Up` but silent for a long
stretch is not "working quietly" - it is **wedged**, holding a container slot and
sometimes spinning a core for nothing.

The container entrypoint turns the headless agent's `stream-json` feed into concise
stdout log lines, so **container-log staleness is a true activity proxy**: a working
engineer streams a line per tool call, a wedged one goes silent.

## What it does

One sweep:

1. Lists the **running** engineers by their `ward=true` + `ward.role=engineer`
   labels (no name-regex; identity rides labels).
2. Computes **idle** = now minus the last container-log timestamp
   (`docker logs --tail 1 --timestamps`). A container that has logged nothing falls
   back to its **start time** - so a fully-wedged PID 1 that never streamed a line is
   still caught (a self-watchdog could not catch that).
3. `docker stop`s any engineer whose idle is at/past the `--idle` threshold
   (default **1h**), subject to the CPU guard.

## The CPU guard

An idle engineer reading above `--max-cpu` (default **5%**) is **spared** as a
possibly-live build or test - engineers commit and push, so a false kill has a real
cost. The guard **only ever spares**: an **unreadable** CPU (a raced container, a
`docker stats` failure) does not disable the reaper. Pass a huge `--max-cpu` to reap
on idle alone.

## Scope: engineers only

Only `ward.role=engineer` is targeted. The interactive roles - `director`,
`advisor`, the director's read-only `session` - are **idle by design** (sitting at a
prompt is normal, not wedged) and left untouched. A longer-grace policy for them can
be a follow-up if an orphan pattern recurs.

## Usage

```bash
ward agent reap                 # sweep once, stop engineers idle >= 1h
ward agent reap --dry-run       # report what would be stopped, stopping nothing
ward agent reap --idle 30m      # tighter threshold
ward agent reap --interval 5m   # standing daemon, sweeping every 5m
```

The default is **one sweep, then exit** - the shape a launchd timer fires on a
schedule. `--interval` turns it into a **standing daemon**; a per-sweep error is
logged and the loop continues.

## Authoring vs rollout

The verb is **authored here**. The **fleet rollout** - a launchd timer firing
`ward agent reap`, or a converged `--interval` daemon - is an **ansible role in
infrastructure**, per the authoring-vs-rollout split. reap is **not** wired into
`ward setup`.

## See also

- [docs/agent.md](agent.md) - the `ward agent` verb family.
- [docs/container-reap.md](container-reap.md) - the in-container teardown backstop.
- [docs/FEATURES.md](FEATURES.md) - inventory.
