# ward agent: WRONG-REPO blind-fire (ward#159)

Part of the headless [pre-flight](agent-preflight.md). Sometimes the pre-flight
read makes it obvious the issue was filed in the wrong place - an ops verb that
belongs on `coily`, an engine change that belongs on `cli-guard`. The agent can
end its read with `WRONG-REPO: owner/repo - <what to file there>` instead of
GO/NO-GO. The point is to **not burn cycles searching**: the verdict comes from
the issue text alone (the prompt tells the agent not to go digging), and ward
acts on it cheaply:

- It **blind-fires** a new issue into the named repo, reusing the source issue's
  text verbatim plus the routing reason and a provenance footer - no search, no
  second agent run. The new issue is flagged "filed blind ... confirm it fits
  before working it," since nobody looked at the target repo first.
- It **comments on the original issue** pointing at the freshly-filed one, with
  the full read folded away, and notes how to override (`--no-preflight`) if the
  routing is wrong.
- Nothing launches on either side. A human (or a later `ward agent` run) picks up
  the routed issue.

Guardrails: the target repo must be in ward's primary-org trust set (the same
gate `work` applies to its own owner), and it can't be the issue's own repo. If
the agent names an untrusted repo, no usable `owner/repo`, or the same repo, the
verdict degrades to a **NO-GO bounce** so a human routes it instead.

## See also

- [docs/agent-preflight.md](agent-preflight.md) - the pre-flight that emits this verdict.
- [docs/agent.md](agent.md) - the `ward agent` verb family and usage.
