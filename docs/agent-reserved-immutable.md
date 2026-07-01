# ward agent: reserved means immutable

A dispatched [engineer](agent-engineer.md) seeds from the issue body **once at
launch** and detaches fire-and-forget. The body rides along as a **frozen
snapshot** taken at dispatch, and the run **never re-reads the issue**. So for as
long as the issue is [reserved](agent-reservation.md) - an engineer in flight, or
the 2h `agentReservationTTL` still open - the issue is **effectively immutable**
to the work in progress. Editing the body or adding an instruction-comment
changes what a human sees and nothing else: it does not reach the running
engineer.

## The operator rule

- **Corrections and scope changes discovered after dispatch go to a new issue,
  dispatched fresh** - never as an edit or a comment on the reserved issue. A
  fresh dispatch re-seeds from the new body, so a new issue is the only channel
  that actually reaches an engineer.
- **A comment reaches only human readers, never the engineer.** ward's own
  reservation ping and pre-flight read are one-directional the same way - ward
  writes the thread, it never feeds the thread back into a run already launched.
- The hold **clears when the reservation does** - the engineer exits or the TTL
  expires - after which the issue is a normal editable ticket again and a
  re-dispatch picks up the current body.

## No enforcement today

Nothing blocks an edit or a comment on a reserved issue. This is **convention,
not a gate**: the immutability is a property of the fire-and-forget seed, not a
lock ward imposes.

## See also

- [docs/agent-reservation.md](agent-reservation.md) - the reservation that bounds the immutable window.
- [docs/agent-engineer.md](agent-engineer.md) - the frozen-snapshot seed the rule follows from.
- [docs/agent-surface.md](agent-surface.md) - filing the fresh correction from the director's surface.
