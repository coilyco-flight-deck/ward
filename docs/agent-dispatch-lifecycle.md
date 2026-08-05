---
doc_goal: Explain Ward's durable public lifecycle for detached broker requests and resulting runs.
---
# Dispatch lifecycle

Every broker launch has one request ID before it crosses the broker boundary.
Ward persists that identity and its public state under
`~/.ward/dispatch-requests`. The originating terminal may disconnect after
acceptance without losing the status trail.

Use the retained surfaces from the host or a broker-connected read-only
director:

```text
ward agent dispatch list
ward agent dispatch status <request-id>
ward agent logs <request-id>
ward agent dispatch prune
ward agent dispatch prune --confirm
```

Add `--json` to list, status, or prune for machine-readable output. Human and
JSON output use the same record. Human output begins with the current state and
ends with the smallest next action.

## Public states

- `queued` is reserved for persisted scheduling intent. Direct dispatch begins
  at `accepted`.
- `accepted`, `launching`, and `running` are active states.
- `cleanup-needed` means the run stopped but Ward could not finish its
  secret-safe archive or retained-container scrub.
- `completed`, `blocked`, `failed`, and `interrupted` are terminal states.
- Orphan and restart details are reason codes on `interrupted`, not extra states.

Internal journal phases remain diagnostic. The public record carries request
ID, timestamps, repository, issue, role, harness, workflow, last transition,
terminal reason, and artifact path. It never needs a credential, prompt body,
transcript body, or unbounded command output. Recovery-only launch arguments
are discarded after the container becomes visible.

## Restart and retention

The broker reconciles every nonterminal record against the stable request ID.
It resumes a safe pre-visibility launch, adopts the one matching container, or
marks an ambiguous missing run `interrupted`. It never invents duplicate work.

Ward does not automatically prune active or `cleanup-needed` records. Terminal
secret-free summaries remain until explicit pruning. `dispatch prune` previews
terminal records older than 30 days by default. `--confirm` removes the selected
journal and its secret-safe dispatch artifact. Use `--older-than` to select a
different age.

See [agent-dispatch-broker.md](agent-dispatch-broker.md) for transport and
[agent-dispatch-recovery.md](agent-dispatch-recovery.md) for internal recovery.
