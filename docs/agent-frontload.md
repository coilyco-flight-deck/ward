---
doc_goal: Explain how dispatch-side context front-loading turns a soft doctrine into an enforced mechanism - the keyword map that hands a fire-and-forget run the right in-clone docs before it plans - and convey why closing the lazy-discovery gap is load-bearing for autonomous run quality, grounded in the real failure that motivated it.
---
# ward agent: front-loading subsystem context ([ward#236](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/236))

`ward agent` has the issue title and body in hand before it detaches, so it does
the dispatch-side half of the agentic-os doctrine "Front-load the context you
know you need": instead of trusting a fire-and-forget run to reach for the right
docs on its own, ward hands them over up front. This is the complement to the
soft doctrine - doctrine nudges, the dispatch path enforces.

The [ward#226](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/226) run is the motivating failure: the agent named its one real unknown
(the ward-kdl guardfile convention) in its own pre-flight, marked it "fully
discoverable in the fresh clone", and then carried on with lazy discovery instead
of reading it. The context was not missing. The clone had it. "Discoverable"
silently became "I'll discover it lazily" and never came back.

## The keyword map

`agentSubsystemPointers` in `cmd/ward/agent_subsystem.go` is a static map from
known ward subsystems to the keywords that name them in an issue and the in-clone
docs/skills to read first:

- `ward-kdl` / `guardfile` / `ops forgejo` - the ward-kdl docs.
- `ward exec` / `ward.yaml` - the exec-verb doc.
- `ward agent` / `headless` / `pre-flight` - the agent docs + skill.
- `reaper` / `container reap` - the container + reap docs.
- plus the hook guard, CI watch, and release docs.

A case-insensitive scan of the title + body picks the matching pointers, each
firing once. Keep keywords specific: a false pointer costs a read, a missing one
costs the point of the issue.

## Where the pointers land

- **In the seed.** A matched headless seed grows a `Front-load before you plan:`
  block listing each subsystem and its paths, with the nudge that "discoverable
  in the clone" is not "read". A plain issue naming no known subsystem is left
  untouched.
- **In the pre-flight.** The same matched pointers are surfaced in the GO/NO-GO
  read, alongside the context gate that asks the agent for a
  `Context to front-load:` line naming the conventions it must read and
  committing to read each before its first edit (see
  [agent-preflight.md](agent-preflight.md)).

## The clone anchor ([ward#384](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/384))

The keyword map assumes the agent knows it *has* a clone to read. The sibling
failure: a goose engineer read the doctrine's abstract "fresh clone", never
connected it to the tree in its own cwd, and reported it "cannot locate the
actual schemas, file layouts, or wiring patterns" - guessing while the real ones
sat one `ls` away.

So every in-container seed now opens with a **clone anchor** (`cloneAnchorLine`):
it names the clone (`owner/repo`), pins it to the agent's cwd, and forbids
assumed conventions when the files are one command away. It rides the in-container seed **only** - the host
pre-flight runs in neutral scratch and says the opposite
([agent-preflight.md](agent-preflight.md)) - and is mirrored into
[AGENTS.container.md](../cmd/ward/containerassets/AGENTS.container.md).

## Scope and follow-ups

The map holds ward-specific paths, so enrichment fires only when the carried
issue is a ward issue (`subsystemPointerRepo`); any other repo is a silent no-op
rather than a pointer at docs the clone lacks. Deferred: a multi-repo keyword
map, per-skill (not just per-doc) pointers, and growing the map as subsystems are
added.

## See also

- [agent.md](agent.md) - the `ward agent` verb family.
- [agent-preflight.md](agent-preflight.md) - the GO/NO-GO read and context gate.
