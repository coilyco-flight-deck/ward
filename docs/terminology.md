---
doc_goal: Define Ward's canonical objects, lifecycle terms, and load-bearing distinctions on one page.
---
# Ward terminology

Use these terms in docs, prompts, command output, and skills.

## Objects

* `Ward` - the governed execution layer for agent runs and repository verbs.
* `ward` - the CLI binary. `warded` is its `ward agent` symlinked face.
* `role` - fixed workflow behavior: `engineer`, `director`, or `qa`.
* `harness` - a typed agent CLI adapter.
* `workflow` - the selected landing policy, not the whole run.
* `cluster` - one broker plus its optional director and peers, identified by a
  repository-independent cluster id.
* `run` - one execution attempt with one container identity.
* `dispatch request` - one durable request accepted or rejected by the broker.
* `reservation` - the issue-thread hold that prevents duplicate work.
* `launch intent` - the local prelaunch lease before a container is visible.
* `terminal outcome` - the final or parked `WARD-WORKFLOW:` state.

## Lifecycle

Work resolves to an issue or ref, then selects a role, harness, and workflow.
Dispatch acceptance may precede launch. Launch applies trust, reservation,
capacity, and host checks before creating a container. The run produces logs
and repository evidence. Teardown drains secret-safe artifacts, proves the
workflow outcome, releases the reservation, and reaps or retains recovery state.

## Distinctions

* Dispatch accepts or forwards. Launch starts host/container execution.
* Broker acceptance does not prove a container or harness is running.
* A launch intent is not a running engineer.
* A workflow is not a run. One issue may receive several runs.
* Process exit is not landing. Landing needs Git or pull-request evidence.
* `submitted`, `merge-ready`, and `done` are different workflow states.
* `blocked` needs outside authority or conditions. `failed` attempted and did not land.
* `stop` targets one run. `reap` applies policy. `cleanup` removes retained state.
* `salvage` preserves a remote branch. `rescue` preserves verified Git objects.
* Read-only means the clone cannot push. It does not remove typed broker authority.
* A cluster is not a repository and is never resolved from repository metadata.
* A release branch is not a published release or install channel.

## Comment compatibility

New Ward-authored machine state starts with `WARD-WORKFLOW:`. Older
`WARDED_WORKFLOW:` and typed `WARD-*` headers remain parser input only.
