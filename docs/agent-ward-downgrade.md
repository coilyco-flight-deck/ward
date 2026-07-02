# ward downgrade guard at dispatch

Guards against a stale in-container reaper ([ward#529](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/529)).

Every container downloads the ward release resolved into `WARD_VERSION` at
dispatch: the dispatching host's own `Version` by default, or an explicit
`--ward-version` pin (env `WARD_AGENT_VERSION`). That same ward runs the
in-process reaper - the [teardown backstop](container-reap.md) that is the last
line against lost or false-salvaged work.

So the reaper is only as fixed as the ward it runs in. A host (or a
`--ward-version` pin) **older** than a reaper-correctness fix silently installs
the older, still-buggy reaper. This is how the [#504](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/504)
false-salvage recurred after `9013c9d` fixed it: not a code regression, but
stale reaper binaries shipped from downgrade pins in the window before the
dispatching host picked the fix up.

## The guard

`buildUpPlan` compares the resolved `WARD_VERSION` against the host's own
`Version` and refuses when the pin is **strictly older**, with a message naming
both versions. The check lives at the dispatcher, not the reaper, so a
known-buggy reaper never ships in the first place rather than being caught after
it has already salvaged a clean tree.

- An equal-or-newer pin passes untouched - upgrading a container's ward is always fine.
- A dev-build host (`Version` is `dev`, or an unparseable tag) has no meaningful
  "older", so the guard never fires - dev hosts dispatch freely.
- `--allow-ward-downgrade` (hidden; see [agent-flags.md](agent-flags.md)) opts
  past the refusal on purpose, for the rare case an operator must pin an older
  ward knowingly.

## See also

- [container-reap.md](container-reap.md) - the reaper this protects.
- [agent-flags.md](agent-flags.md) - the `--ward-version` / `--allow-ward-downgrade` flags.
