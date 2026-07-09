---
doc_goal: Explain why ward's own GitHub client routes every call to REST and stays off the tighter GraphQL budget, dogfooding the same ghGraphQLTrap hint it presses on agents, and justify why no client-side backoff is wired given the small per-run call volume of a warded GitHub run.
---
# ward's GitHub client stays off the GraphQL budget ([ward#466](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/466))

GitHub's REST and GraphQL APIs carry **separate** rate budgets, and the GraphQL one is
the tighter, point-metered lane. ward's own GitHub client (`cmd/ward/github_ops.go`)
stays conservative by preferring REST routes so a [warded GitHub run](agent-github.md)
does not burn the tighter budget needlessly.

## What routes where

- **Reads** - the issue fetch and the comment-thread pull go through `gh api
  /repos/{o}/{r}/issues/{n}` and `.../comments`, not `gh issue view --json` (GraphQL).
  The comment pull passes `--paginate -f per_page=100`, so it merges every page into one
  array while a short thread (the common case) still costs a single request.
- **State flips** - `closeIssue` / `reopenIssue` PATCH `state` through `gh api`, not
  `gh issue close` / `gh issue reopen`, which route through a GraphQL mutation.
- **Writes** - the issue create + comment posts stay on `gh issue create` / `gh issue
  comment`: those are already REST POSTs and lean on `--body-file` to keep the signed
  body off argv.

## Why no backoff

Per-run GitHub call volume is small - a handful of reads plus the reservation, outcome,
and close - so no client-side rate-limit backoff is wired beyond the existing
[reservation-post retry](agent-reservation.md). The win here is staying off the GraphQL
budget entirely, not throttling a load that was never heavy.

## See also

- [agent-github.md](agent-github.md) - the GitHub-hosted run end to end.
