# broker

The broker is the root credential relay for the read-only director surface.

- Bootstrap starts `ward container broker` as root before the director drops
  privilege, waits for `/run/ward/broker.sock` (or `$WARD_BROKER_SOCK`), and
  group-owns it for the agent.
- Its daemon output goes to `/run/ward/broker.log` (or `$WARD_BROKER_LOG`), not
  the shared director TUI.
- The dropped director receives `WARD_BROKER_SOCK`, but never `FORGEJO_TOKEN`.
  The root bootstrap retains that value only for the broker and reaper.
- Ward's native Forgejo control-plane adapter resolves its in-process
  authentication through the broker on a director surface. Write-tier
  operations are authorized by the broker; out-of-tier mutations are refused.

The broker keeps the credential out of argv, logs, audit rows, doctrine, and
the dropped agent environment. It is part of the agent surface contract, not a
generic network helper.

## Rotation

Before it vends the in-process credential needed by a brokered director read,
the broker refreshes from the configured SSM source. If Forgejo rejects a
root-held credential during an authorized write, it also refreshes and retries
once. The director never needs to fetch, print, or inject the replacement. If
that root refresh fails, the broker returns a clear recycle-the-director error
rather than a raw Forgejo 401; exit the surface so the director heartbeat can
relaunch it.

## See also

- [agent-director.md](agent-director.md) - the surface the broker supports.
