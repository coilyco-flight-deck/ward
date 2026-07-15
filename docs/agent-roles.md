---
doc_goal: Collapse the role roster into one durable page that names the three startup roles and the contracts that make them distinct.
---
# ward agent roles

`ward agent` has three startup roles.

Their definitions live in ward-owned embedded role-definition KDL, with fleet
config acting only as an overlay.
- `engineer` - implements a ticket end to end and detaches.
- `director` - supervises the backlog and surfaces a read-only session.
- `qa` - inspects a run or branch and posts a structured verdict comment.
- Each startup role carries a shipped execution limit, except `director`, which
  stays interactive and has none.

The semantic model is role-based, not guardfile-based. Guardfile membership
only controls which host-side capabilities a role can use.
## What stays distinct

- `engineer` is the only role that carries implementation work.
- `director` is the only role that owns the read-only surface.
- `qa` is opt-in and writes verdicts, not code.

## Role details

### engineer

The engineer is the detached worker. It receives an issue, a repo, or a prompt
and does the actual implementation work inside the container.

- It always detaches.
- It is the role that lands code.
- It is the one `warded` starts when the user only gives a ref.
- Its shipped execution limit is 90 minutes.

### director

The director is the supervising lane.

- It can read logs and inspect the fleet.
- It opens the read-only session by default.
- It can keep a backlog moving when `--burndown` or `--drain` is set.
- It has no execution limit.

### qa

The QA role is a light-weight inspection pass.

- It runs only when asked.
- It writes a verdict comment.
- It does not edit the code under review.
- Its shipped execution limit is 30 minutes.

## What the role word means

The role is the first noun after `warded` or `ward agent`.

- `warded #98` means engineer.
- `warded director --repo owner/name`, `warded director owner/repo#98`, and `warded director #98` mean the supervisory lane with repo, issue, or current-checkout scope.
- `warded qa #98` means structured inspection.

## See also

- [agent.md](agent.md) - the entrypoint overview.
- [agent-harnesses.md](agent-harnesses.md) - the harness axis.
- [agent-lifecycle.md](agent-lifecycle.md) - launch and preflight.
- [agent-director.md](agent-director.md) - the director lane.
