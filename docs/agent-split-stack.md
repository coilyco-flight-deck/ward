---
doc_goal: Explain tracker and git authority without a runtime policy bundle.
---
# Split-stack agent dispatch

Issue tracking and git hosting are separate authorities. A full issue URL pins
the tracker for the whole run, including brokered launch. Ward does not reduce
it to `owner/repo#N` before forwarding.

Ward's typed repository authority defaults select checkout, tracker, landing,
and known mirrors. A full tracker URL always wins for that invocation. A role
label cannot change any of these authorities.

Deployment-specific authority belongs below Ward in the deployment layer. Ward
does not fetch it from a reference repository at launch time.

See [agent-lifecycle.md](agent-lifecycle.md) and
[enforcement-boundary.md](enforcement-boundary.md).
