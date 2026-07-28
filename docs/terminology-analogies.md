---
doc_goal: Provide concise audience-oriented analogies for Ward without making any single metaphor mandatory.
---
# terminology analogies

Use these as explanation frames, not replacement naming systems.

## Direct

Ward is a guarded CLI and agent control plane. It validates repo verbs, launches
coding agents in fresh least-access containers, records what happened, and
lands or preserves work through explicit workflows.

## Platform

Ward accepts work, applies admission checks, enforces capacity and
backpressure, starts isolated workers, tracks state in a durable ledger, and
reconciles outcomes through a supervisor.

## Software Delivery

Ward carries an issue through branch creation, implementation, test/review
gates, PR or main landing, issue comments, and release promotion evidence.

## Security Boundary

Ward keeps authority explicit. Repo config controls local dev verbs; tracker
state controls reservations; broker and host launch paths control dispatch;
containers get only declared mounts and credentials; read-only director
surfaces can supervise without pushing their clone.

## Process Supervision

Ward starts child processes in containers, records their identity and output,
distinguishes live, stale, exited, and failed-before-start states, and uses
stop, reap, cleanup, salvage, and rescue paths to recover without losing
committed work.

## Flight Deck

Ward dispatches work, launches a run, watches active fleet visibility, permits
intervention or abort through stop/reap, keeps flight recording through logs
and audit rows, preserves stranded work through salvage branches or rescue
artifacts, and lands only when the selected workflow proves the change reached
its destination.

"Warded flight ops" is a useful shorthand for orchestration when the audience
already understands that the analogy does not rename the state machine.
