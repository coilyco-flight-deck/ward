# director on-demand surface (ward#409)

Drain is not the only time a human watching the [director](agent-director.md) heartbeat wants
the floor. When a tick **cannot schedule** - every engineer slot busy (`avail <= 0`) while work
is still **in flight** - the sleep line would otherwise read as an idle stall (it confused a
live operator). Instead the heartbeat offers an **on-demand surface** during that sleep window.

## The offer

On a terminal, a slots-full tick prints a keypress affordance - "all N engineer slot(s) busy
and work still draining - press Enter to drop into an interactive session now, else polling in
Ns..." - and races an Enter read against the poll interval:

- **Enter** hands off through the **same** `directorSurface` path and flag-forwarding
  (`directorSurfaceArgv`) as the drain surface, so container/harness parity is identical. On
  exit the loop **re-polls** the still-draining lane rather than stopping.
- **No keypress** within the poll interval resumes the heartbeat silently, same as a plain sleep.

## Only when it can't schedule

The prompt fires on exactly one condition (`directorSlotsFull`): no free slot **and** engineers
still in flight. Every quiet case keeps the quiet sleep:

- A **free slot** (`avail > 0`), whether or not the LLM held the tick.
- **Nothing queued and nothing in flight** - that is the existing drain to surface path.

## Headless stays silent

The read uses the controlling terminal on its **own** file descriptor (`/dev/tty`), released
before the surface container attaches to stdin, so the deadline and non-blocking mode never
touch fd 0. A **headless / no-terminal** director (gated by `terminalAttached`, like
`directorSurface` itself) keeps sleeping silently - no prompt, no stdin read. `--dry-run` and
`--print` never reach the heartbeat, so they never prompt either.

## See also

- [agent-director.md](agent-director.md) - the heartbeat and its drain to surface path.
- [agent-surface.md](agent-surface.md) - the read-only session both paths drop into.
