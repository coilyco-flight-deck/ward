---
doc_goal: Give a supervising harness the two machine-readable signals a fire-and-forget guarded run emits - the dispatch exit code and the drained meta.json outcome - as a drift-tested contract it can branch on, and convey that this enum stability is what makes headless agent governance auditable rather than a black box.
---
# ward agent: the machine-readable dispatch contract ([ward#485](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/485))

A `warded owner/repo#N` dispatch is fire-and-forget, so a supervising harness
(ward's own [director](agent-director.md) is one) can't watch the run - it reads
two machine-readable signals instead: the **process exit code** at dispatch (did a
container launch, and if not why), then the drained **`meta.json` outcome** once
the run ends (how it landed). Both enums live in code and are drift-tested against
this page, so `if exit == X` / `switch outcome` handling written from here stays
honest.

## Dispatch exit codes

Three buckets: `0` **launched**, `1` **error** (dispatch broke), and `2`-`5`
**refused** (a gate declined to launch). `0` means a container detached (then poll
its `meta.json` outcome below); every non-zero code is a distinct
"nothing launched here" ending:

* `0` - **launched** - a container detached (also a `--print` dry run, and a pre-flight that could not read a verdict so failed open); the run's fate is the `meta.json` outcome, not the exit code.
* `1` - **launch-failure** - bring-up itself failed, or the issue could not be resolved / fetched; a subprocess (docker) with no more specific code also surfaces here.
* `2` - **untrusted-owner** - the owner trust gate refused the ref before any container spun up ([agent-trust-gate.md](agent-trust-gate.md)).
* `3` - **reservation-conflict** - another live run already holds the issue; retry when it finishes or pass `--force` ([agent-reservation.md](agent-reservation.md)).
* `4` - **no-go** - the interactive pre-flight returned NO-GO (or an unusable WRONG-REPO bounced to a human); nothing launched, a comment was posted ([agent-preflight.md](agent-preflight.md)).
* `5` - **wrong-repo** - the interactive pre-flight blind-fired the work into another trusted repo; nothing launched here ([agent-wrong-repo.md](agent-wrong-repo.md)).

Codes `4` and `5` only ever arise from the **interactive** pre-flight, which is
skipped without a TTY (scripted / piped, `--print`, `--no-preflight`) - so a
headless supervisor dispatching into a pipe sees only `0`/`1`/`2`/`3`. That
host pre-flight is slated for removal ([ward#162](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/162)); once it is gone the NO-GO /
WRONG-REPO judgement moves in-container and is reported through the `meta.json`
outcome below, not a dispatch code. `0`/`1`/`2` line up with the shared cli-guard
exit-code contract (success / generic / policy-denied); `3`-`5` are `ward
agent`-specific. Source of truth: `dispatchExitCodes` in `cmd/ward/agent_exit.go`.

## meta.json outcome enum

The [drained](agent-observability.md) `meta.json` records an `outcome` inferred
from the reaper's console markers (`classifyReapOutcome`). The **complete enum** -
both the success half and the failure half - is these four:

* `pushed-to-main` - clean integration, the work landed on `main`.
* `ward-salvage` - a conflict, secret/vendored-content scan finding, rejected push, or dead-PAT auth failure routed the work to a `ward-salvage/<id>` branch instead ([container-reap.md](container-reap.md)).
* `nothing-to-reap` - the tree was already clean and pushed; the reaper found nothing to do.
* `unknown` - no reaper marker matched: a crash, an externally-stopped run, or an auth smoke-test abort ([ward#222](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/222)) that never reached a teardown verdict.

Source of truth: `reapOutcomeValues` in `cmd/ward/agent_log_drain.go`.
`TestDispatchContractDocumented` fails if either enum drifts from this page.

## See also

- [agent-observability.md](agent-observability.md) - how the `meta.json` is drained and where it lands.
- [agent-preflight.md](agent-preflight.md) - the GO / NO-GO / WRONG-REPO pre-flight behind codes `4`/`5`.
- [container-reap.md](container-reap.md) - the teardown that decides the outcome.
