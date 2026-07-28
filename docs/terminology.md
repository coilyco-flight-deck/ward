---
doc_goal: Keep Ward's canonical vocabulary entry point small while routing detailed inventory, ambiguities, model, and analogies to focused pages.
---
# ward terminology

This is the canonical entry point for Ward vocabulary. Use it before changing
docs, prompts, command output, or skills that name Ward objects, states, or
transitions.

Ward can use metaphor, including flight-deck language, but operational meaning
wins. Define the behavior first; use metaphor only when it helps explain that
behavior.

## Normative Terms

- `Ward`: the governed execution layer for unattended coding agents and audited
  repo verbs.
- `ward`: the CLI binary.
- `warded`: the public symlinked face for `ward agent`.
- `cli-guard`: the policy and routing engine Ward consumes.
- `aosguard`: the separate AOS operator CLI, not Ward runtime policy.
- `work`: the requested change or inspection, usually an issue/ref or filed
  freeform issue.
- `role`: `engineer`, `director`, or `qa`.
- `harness`: the agent CLI/runtime family, such as `claude`, `codex`, `goose`,
  or `opencode`.
- `workflow`: the selected landing policy, not the whole run.
- `run`: one execution attempt for a role, harness, ref, workflow, and
  container identity.
- `reservation`: the issue-thread hold that prevents duplicate work.
- `launch intent`: the prelaunch lease before a running engineer is visible.
- `running engineer`: a visible engineer container carrying Ward labels.
- `terminal outcome`: a machine-readable `WARD-WORKFLOW:` status that ends or
  parks the run's hold.

## Load-Bearing Distinctions

- `dispatch` is not `launch`: dispatch accepts or forwards a request; launch
  starts the host/container execution path.
- `broker accepted` is not `container visible`.
- `launch intent` is not `running engineer`.
- `run` is not `issue`: an issue can receive many runs.
- `workflow` is not `run`: workflow is the landing policy and comment family.
- `landed` is not `process exited`: landing requires repo or PR evidence.
- `submitted` and `merge-ready` are not `done`.
- `blocked` is not `failed`.
- `backpressure` is not a terminal failure.
- `stop` is not `reap`.
- `harness` is not `role`.
- `read-only` is not powerless: the director can supervise and broker work
  without pushing its clone.
- `release branch` is not `Forgejo release`.

## Comment Markers

`WARD-WORKFLOW:` is the canonical first line for new Ward-authored workflow,
reservation, review, QA, route, and terminal issue comments.

`WARDED_WORKFLOW:` and older typed `WARD-OUTCOME:`, `WARD-RESERVATION:`,
`WARD-DISPATCH:`, `WARD-QA:`, `WARD-STATUS:`, `WARD-REAP:`, and
`WARD-TRIAGE:` headers are parser compatibility only.

## Supporting Pages

- [terminology-inventory.md](terminology-inventory.md) - sampled vocabulary by
  concept.
- [terminology-ambiguities.md](terminology-ambiguities.md) - overloaded and
  collision-prone terms.
- [terminology-model.md](terminology-model.md) - object, lifecycle, and
  ownership model.
- [terminology-analogies.md](terminology-analogies.md) - audience-oriented
  explanation frames, including flight-deck framing.

## Change Rule

Add or update terminology here or in the supporting terminology pages before
spreading a new synonym through docs, prompts, command output, or skills. Do not
rename stable CLI commands as part of a terminology cleanup; file a separate
issue for compatibility churn.
