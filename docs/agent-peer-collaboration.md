---
doc_goal: Explain generic composed-agent launches and authenticated broker messages without defining application-specific roles.
---
# Generic agent collaboration

Ward can host composed agents without adding each role to its fixed workflow
roster. The broker treats role slugs as opaque context selectors.

`--repo owner/name` selects the repository-backed plan. An existing `--cluster`
without `--repo` selects repository-free collaboration. Both take role context
from the peer's AOS-generated bundle.

## Launch a peer

From the host, attach a repository-free peer to an existing cluster:

```bash
ward agent run \
  --cluster codex-ab45 \
  --harness codex \
  --role story-architect \
  --context-bundle /path/to/story-architect-bundle \
  "Shape the premise, then ask critic for pressure tests."
```

`ward agent run` accepts any safe lowercase role slug. It requires no issue.
The repository-free plan requires an existing cluster and a bundle matching
the selected role and harness. It does not infer or clone a Git target, choose
a repository workflow, or inject Forgejo credentials. The role grants no
credentials, mounts, network access, or landing workflow.

Bundle and substrate inputs are read-only. `/scratch`, private harness homes,
and runtime state are writable. `--repo` opts into the unchanged repo plan.

The operator normally omits a peer id. Broker admission mints
`<role>-<ab12>`, records it in the durable request journal before launch, and
returns both the cluster id and peer id. The peer id stays stable for that
admission and is immediately usable with `message send`. An explicit
`--agent-id` remains a compatibility override, not a normal-flow input.

A peer capability may launch another generic peer. It cannot select the fixed
engineer or QA workflows or call privileged broker actions.

Broker-launched descendants inherit the admitting broker's cluster id. A new
role-specific context bundle still needs a host launch because a container-only
bundle path cannot become a new host read-only bind.

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
The same returned id addresses direct messages, `ward agent logs <peer-id>`,
and `ward agent stop <peer-id>`. Stopping a peer retires its active admission.

## Context bundles

Host-launched composed peers retain their own validated context bundle when
they join a broker. The bundle is context only and cannot grant repository or
Forgejo authority. Nested agents cannot forward a container-only bundle path
as a new host bind. Launch a separately composed role through AOS on the host.
