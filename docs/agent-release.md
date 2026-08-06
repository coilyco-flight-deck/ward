---
doc_goal: Define Ward's provider-neutral Director-to-Ops release contract without granting deployment authority.
---
# Agent release contract

Ward transports one typed handoff from an authenticated Director to one exact
broker-minted Ops peer. It records immutable identifiers and evidence digests.
It contains no deployment implementation and grants no authority.

Repository deploy and verify operations must already exist on the guarded Ops
surface. The candidate names them with safe symbolic IDs. It cannot carry argv,
scripts, paths, URLs, environment values, credentials, or raw payloads.

## Candidate

The candidate JSON input contains:

* `application_repository` and its full lowercase `application_commit`.
* An immutable SHA-256 `artifact_digest` and symbolic `environment` ID.
* `deploy_repository` and its full lowercase `starting_deploy_commit`.
* A canonical `originating_ticket` in `owner/repo#N` form.
* Symbolic `deploy_operation` and `verify_operation` IDs.

```bash
ward agent release candidate --to ops-ab12 --file candidate.json
```

The broker stamps sender, recipient, cluster, Ward run, optional dispatch
request, candidate ID, creation time, and content hash. It also mints the first
attempt ID. Changing any candidate field requires a new candidate.

## Result

Result JSON names the candidate and attempt, one classification, a symbolic
reason code, and at least one SHA-256 evidence digest. Ops submits it with:

```bash
ward agent release result --file result.json
```

The broker accepts it only from the candidate's exact Ops peer. Outcomes are:

* `verified` requires the new pushed `deploy_commit`.
* `rejected` means refusal before mutation and carries no deploy-state commit.
* `restored` requires `restored_commit` equal to the candidate's starting
  commit and carries no new deploy-state commit.
* `blocked` means an external dependency prevented mutation from starting and
  carries no deploy-state commit.
* `indeterminate` means mutation or push state is ambiguous. It carries no
  deploy-state commit and blocks automated retry pending Ops reconciliation.

Harness, transport, and broker failures remain Ward lifecycle failures.

## Retry and read

`rejected`, `restored`, and `blocked` may receive a new attempt while the
starting revision still matches. `verified` and `indeterminate` cannot retry.

```bash
ward agent release retry --candidate <id> --starting-deploy-commit <full-commit>
ward agent release receive
ward agent release receive --after <record-id>
```

Ward stores candidate, attempt, result, reason, correlation, and evidence digest
records in `release.jsonl` beside the Ops peer's dispatch artifact. Read it by
dispatch request, peer, or originating ticket:

```bash
ward agent logs <request-id> --artifact release
ward agent logs ops-ab12 --artifact release
ward agent logs acme/app#42 --artifact release
```

Only validated identifiers, revisions, hashes, and digests enter the sidecar.
Ward rejects configured secret-bearing values and never persists the input file
or raw deployment and verification output. See
[generic collaboration](agent-peer-collaboration.md),
[dispatch broker](agent-dispatch-broker.md), [observability](agent-observability.md),
and [operations](agent-ops.md).
