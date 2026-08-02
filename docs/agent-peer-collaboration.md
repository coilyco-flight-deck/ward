---
doc_goal: Explain generic composed-agent launches and authenticated broker messages without defining application-specific roles.
---
# Generic agent collaboration

Ward can host composed agents without adding each role to its fixed workflow
roster. The broker treats role slugs as opaque context selectors.

When a matching director broker is already running for the target repository
and harness, a host-launched generic run joins that broker automatically. Its
own AOS-generated context bundle remains the source of role context.

## Launch a peer

From a brokered agent:

```bash
ward agent run \
  --role story-architect \
  "Shape the premise, then ask critic for pressure tests."
```

`ward agent run` accepts any safe lowercase role slug. It requires no issue.
The generic command supplies a read-only one-shot lifecycle. The role does not
select credentials, mounts, network access, or a landing workflow.

The operator normally omits a peer id. Broker admission mints
`<role>-<ab12>`, records it in the durable request journal before launch, and
returns both the cluster id and peer id. The peer id stays stable for that
admission and is immediately usable with `message send`. An explicit
`--agent-id` remains a compatibility override, not a normal-flow input.

A peer capability may launch another generic peer. It cannot select the fixed
engineer or QA workflows or call privileged broker actions.

## Exchange messages

```bash
ward agent message send --to critic "Pressure-test the second act."
ward agent message receive --json
```

The broker derives a capability for each launched agent ID. It authenticates
that capability and stamps `from` itself. A caller-supplied sender name is not
trusted. Recipients may be one agent ID or `*` for the broker group.

Messages are durable under Ward's broker state, may carry an optional
conversation ID, and are bounded to 64 KiB. `receive --after <message-id>`
supports incremental reads.

The launch prompt names the current agent ID and these commands when the
message surface is available. Ward defines the transport and capability
boundary only. The application remains free to choose roles, topology, work,
and conversation patterns.

Cluster status exposes each active `ward.peer` id beside its role and harness.
Failed launches move their admission out of the active roster, and broker
restart reconciliation restores the accepted identity from durable state.

## Context bundles

Host-launched composed peers retain their own validated context bundle when
they join a broker. Nested agents cannot forward a container-only bundle path
as a new host bind. Launch a separately composed role through AOS on the host.
