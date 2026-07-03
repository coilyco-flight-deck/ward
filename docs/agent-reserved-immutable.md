---
doc_goal: Land the operator rule that a reserved issue is frozen to the run carrying it - a correction goes to a new issue dispatched fresh, never a comment or edit - and explain the best-effort forge lock that makes the rule visible and, on GitHub, enforced.
---
# ward agent: reserved means immutable

A dispatched [engineer](agent-engineer.md) seeds from the issue body **once at
launch** and detaches fire-and-forget. The body rides along as a **frozen
snapshot** taken at dispatch, and the run **never re-reads the issue**. So for as
long as the issue is [reserved](agent-reservation.md) - an engineer in flight, or
the 2h `agentReservationTTL` still open - the issue is **effectively immutable**
to the work in progress. Editing the body or adding an instruction-comment
changes what a human sees and nothing else: it does not reach the running
engineer.

The carried issue identity is likewise frozen at dispatch. Ward now spells that
identity out explicitly in the seeded prompt and pre-flight read so adjacent
issue numbers cannot bleed into one another.

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

## Best-effort enforcement ([ward#494](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/494))

The reservation now spells the rule out where a human will read it and, where the
forge allows, backs it with a real lock:

- **The reservation comment is the road-block.** Every `🔒 Reserved` marker
  carries an explicit directive: *do not comment on or edit this issue to steer
  the run while it is reserved; a correction goes to a new issue, dispatched
  fresh.* On a forge whose API can't lock a conversation, this loud in-thread
  directive is the whole gate - convention made visible.
- **Where the API supports it, ward locks the conversation.** A GitHub-hosted run
  seals the issue (`PUT /issues/{n}/lock`) when it reserves, so a non-collaborator
  is blocked outright and a write-access architect meets a deliberate "unlock to
  comment" road-block - the friction the issue asked for. **Forgejo (gitea-1.22
  API compat) exposes no lock leaf** - `is_locked` is read-only over the API - so a
  Forgejo run leaves the comment above as the road-block and logs that it did.
- The lock is **best-effort**: it never fails the reservation, and a
  [pre-launch death](agent-reservation.md) that releases the hold also unlocks
  (where the forge can), so a clean retry lands on an open thread.

Note the lock is friction, not an absolute denial: a repo owner can always unlock
and comment. The point is the conscious pause, not an unbreakable wall - the run
still never re-reads the issue either way, so the discipline stands.

## See also

- [docs/agent-reservation.md](agent-reservation.md) - the reservation that bounds the immutable window.
- [docs/agent-engineer.md](agent-engineer.md) - the frozen-snapshot seed the rule follows from.
- [docs/agent-surface.md](agent-surface.md) - filing the fresh correction from the director's surface.
