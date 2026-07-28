# broker

The current Compose director uses its privileged sibling broker for native
Forgejo operations.

- Only the broker service receives `FORGEJO_TOKEN`.
- The director service receives its selected harness credential and the
  per-stack broker capability in a separate env file. Its container
  environment and projected home contain no transferable Forgejo credential.
- Ward's native Forgejo client sends bounded request data to the broker. It
  never asks the broker to return the token.
- The broker rechecks the director role, the exact native route allowlist, the
  owner prefix, the repository shape, request size, query keys, and response
  size before or during every request.
- The broker connects only to Ward's fixed Forgejo API base. It is not a
  general HTTP proxy or a generic secret-discovery service.
- Broker capability rejection, upstream credential rejection, network failure,
  and policy denial remain distinct errors.
- Ward removes the temporary host env files when the attached director run
  ends. The broker credential remains only in the supervised broker
  container's process environment until that container is reaped.

The older root Unix-socket broker remains as a compatibility path for older
read-only containers and its narrow issue-write surface. Current Compose
directors use `WARD_DISPATCH_BROKER_ADDR` instead of receiving
`WARD_BROKER_SOCK`.

The broker keeps the credential out of director argv, logs, audit rows,
doctrine, projected files, and environment. It is part of the agent surface
contract, not a generic network helper.

## Rotation

The Compose broker snapshots the host-resolved credential when the stack
starts. If Forgejo rejects it, the broker returns an `auth` failure that tells
the operator to recycle the director. Hot refresh is deliberately outside this
contract. The director never fetches, prints, or injects a replacement.

## See also

- [agent-director.md](agent-director.md) - the surface the broker supports.
