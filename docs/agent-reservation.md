---
doc_goal: Define canonical reservation authority, launch intents, release semantics, redispatch, stale cleanup, and disposable cache recovery.
---
# Agent reservations

The issue thread is canonical reservation authority. A local launch-intent
record prevents pre-container duplication and helps display startup state, but
it is disposable cache and is not a running engineer.

## Reservation and release

* A fresh reservation prevents a competing launch for the same issue.
* A release marker at or after the latest reservation retracts the hold.
* Terminal or parked `WARD-WORKFLOW:` state also retracts it immediately.
* A launch that fails before visibility releases its intent immediately. The
  TTL is only an orphan backstop.
* The reaper skips its release comment when a newer reservation proves a
  follow-up run already owns the issue.
* Once inactive, Ward may remove stale reservation and dispatch telemetry
  comments while retaining the meaningful workflow record.

A dispatch that still collides with live work starts nothing and records a
needs-redispatch marker. Queue and health surfaces report it. A harness-native
goal decides whether to try again, and every retry reapplies launch gates.

## Stale launch recovery

1. Run `ward agent list --json` and read the issue thread.
2. If no engineer is visible and the launch intent is stale, preview
   `ward agent stop owner/repo#N --print`.
3. Run the stop to clear the confirmed Ward-owned launch and reservation state.
4. Re-run list before dispatching again.

Never clear a reservation for visible live work. When the issue thread is
already correct and only local cache is stale, run `ward agent reservations
clear`. It removes and recreates `~/.ward/agent-reservations` wholesale.

## See also

* [agent-lifecycle.md](agent-lifecycle.md) - launch checks.
* [agent-ops.md](agent-ops.md) - list, stop, and cache commands.
