---
doc_goal: Make clear how the advisor's structured emit deterministically fans a cross-repo design into one trust-gated issue per repo plus an index comment, so a reader trusts that ward - never the agent - files under its bot identity only in repos the operator's trust gate allows.
---
# advisor ref mode: structured emit and cross-repo fan-out

The advisor [ref mode](agent-advisor.md) does not ask the research pass for verbatim
comment markdown. It asks for a **single fenced ```json block** - a short `summary` plus
an ordered list of `{repo, title, body}` issue specs - and ward parses that block and
picks the output shape by scope (ward#424).

## The two shapes

- **Single comment (the common case).** When the plan names **0 or 1 distinct repo** - a
  direct answer, or work that belongs on the asked issue itself - ward folds the summary
  (and any lone spec) into **one comment on the source issue**, exactly like before. No
  redundant child issue is filed for single-repo work.
- **Fan-out.** When the plan spans **2+ distinct trusted repos**, ward **creates one
  issue per repo** deterministically (never the agent), in dependency order, each body
  cross-linked to its upstream, then posts **one index comment** on the source issue
  linking the children with the summary. The source issue stays the landing spot.

An unparseable or prose read falls back to posting the raw research verbatim, so the
structured contract never hard-fails a reply.

## Per-repo trust gate

Issue creation is deterministic and gated **per target repo**: every spec's owner must
pass the same `ownerAllowed` / primary-org check as the source ref (the [owner trust
gate](agent-trust-gate.md) - the security-relevant
part is that fan-out files under ward's bot identity in repos the operator never named). A spec
naming an untrusted owner or a malformed slug is **dropped, never created**, and the drop
is surfaced in the index (or single) comment rather than silently swallowed. If the gate
drops the plan below 2 distinct repos, ward falls back to a single comment and notes what
it withheld.

## Sequencing

Cross-repo designs usually have an order (A enables B enables C). The research emits the
specs already ordered, and ward files them in that order, stamping each child body with a
provenance footer that names the source issue, its position (`part N of M`), and its
upstream dependency ref. Combined with the agent's own in-body dependency prose, an
engineer dispatched against child B knows it waits on A.

## See also

- [docs/agent-advisor.md](agent-advisor.md) - the advisor role and its two modes.
- [docs/agent-attribution.md](agent-attribution.md) - how each posted body is signed.
- [docs/FEATURES.md](FEATURES.md) - inventory of what ships today.
