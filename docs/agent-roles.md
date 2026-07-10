---
doc_goal: Collapse the role roster into one durable page that names the four startup roles and the contracts that make them distinct.
---
# ward agent roles

`ward agent` has four startup roles.

- `engineer` - implements a ticket end to end and detaches.
- `director` - supervises the backlog and surfaces a read-only session.
- `advisor` - answers questions and writes no code.
- `qa` - inspects a run or branch and posts a structured verdict comment.

The semantic model is role-based, not guardfile-based. Guardfile membership
only controls which host-side capabilities a role can use.

## What stays distinct

- `engineer` is the only role that carries implementation work.
- `director` is the only role that owns the read-only surface.
- `advisor` is the only role whose job is response and triage.
- `qa` is opt-in and writes verdicts, not code.

## Role details

### engineer

The engineer is the detached worker. It receives an issue, a repo, or a prompt
and does the actual implementation work inside the container.

- It always detaches.
- It is the role that lands code.
- It is the one `warded` starts when the user only gives a ref.

### director

The director is the supervising lane.

- It can read logs and inspect the fleet.
- It can keep a backlog moving.
- It is the role that owns the read-only session after drain.

### advisor

The advisor answers without changing implementation state.

- It posts comments or a structured reply.
- It is useful for triage and design questions.
- It does not land code.

### qa

The QA role is a light-weight inspection pass.

- It runs only when asked.
- It writes a verdict comment.
- It does not edit the code under review.

## What the role word means

The role is the first noun after `warded` or `ward agent`.

- `warded #98` means engineer.
- `warded director #98` means the supervisory lane.
- `warded advisor #98` means the answer-only path.
- `warded qa #98` means structured inspection.

## See also

- [agent.md](agent.md) - the entrypoint overview.
- [agent-harnesses.md](agent-harnesses.md) - the harness axis.
- [agent-lifecycle.md](agent-lifecycle.md) - launch and preflight.
- [agent-director.md](agent-director.md) - the director lane.
