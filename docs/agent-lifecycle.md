---
doc_goal: Define the source-aligned agent path from target resolution through launch checks, container visibility, workflow evidence, and teardown.
---
# Agent lifecycle

1. Resolve an issue, pull request, repository scope, or freeform task.
2. Resolve role, harness, workflow, context bundle, image, and version.
3. Validate config, target authority, role/harness compatibility, and trust.
4. Re-read reservation, branch continuation, pull-request backpressure, and
   capacity because these can drift while dispatch waits.
5. Run harness install and host preflight, then launch-adjacent image, network,
   auth, and smoke checks.
6. Persist the reservation and launch intent, create the ephemeral container,
   and start the harness.
7. Record logs, candidate evidence, workflow outcome, reservation release, and
   secret-safe teardown artifacts.

Broker request parsing and transport checks happen before forwarding. The
preflight owns trust, issue-facing refusal, and host checks. Driftable
reservation, branch, PR-pressure, and capacity gates are checked at broker
admission and again immediately before launch.

## Bypasses and preview

* Engineer `--print` renders the resolved host plan and starts nothing. Local
  and brokered previews skip launch admission, staging, reservation,
  backpressure, capacity, Docker-readiness, preflight, and recovery. Brokered
  preview is a synchronous plan request with no durable dispatch state.
* `--skip-smoke-test` skips only the in-container harness smoke test.
  `WARD_SMOKE_TEST_SKIP=1` is its direct-launch environment alias.
* `--skip-preflight` also bypasses host preflight, reservation recheck wait,
  launch-adjacent probes, and review gate. It does not create authority.
* `--override-reservation` and `--override-capacity` are separate explicit
  recoveries. Never use either to collide with visible live work.

## Refusal outcomes

Ward keeps launch failure, untrusted owner, reservation conflict, no-go,
wrong repository, closed issue, and mode ceiling distinct in errors, exit
codes, dispatch artifacts, and tracker records.

## Split repositories and credentials

Tracker, checkout, and landing providers may differ. Ward resolves each typed
adapter independently. Host credentials are resolved before launch. Engineer
and QA receive only the Git credential channel plus role-bound broker access.
Director receives its selected harness channel and master broker capability,
but no transferable forge credential.

## Completion

A process exit is not completion. The selected workflow decides whether main,
a pull request, or a remote branch is required. Teardown preserves unlanded
committed Git history before cleanup and never reopens work already proven on
the landing target.

## See also

* [agent-reservation.md](agent-reservation.md) - reservation authority.
* [agent-dispatch-broker.md](agent-dispatch-broker.md) - broker milestones.
* [agent-workflow.md](agent-workflow.md) - landing evidence.
* [container-lifecycle.md](container-lifecycle.md) - drain and recovery.
