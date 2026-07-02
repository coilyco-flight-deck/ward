# GitHub token path

When `warded` is pointed at a GitHub issue URL, GitHub-side comments and pull
request creation use a `GITHUB_TOKEN` from the private env-file. That token stays
out of argv/audit, and it needs enough repo scope to read the issue thread, post
comments, and open a PR in the target repo.

Use this path when GitHub is the public front door and Forgejo is the canonical
mirror behind it.

## Token source selector (`WARD_GITHUB_TOKEN_SOURCE`)

How ward provisions that token is **operator-selectable** ([ward#533](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/533)), chosen by
`WARD_GITHUB_TOKEN_SOURCE` and defaulting to `env`. In every mode resolution stays
**host-side** and env-only in-tree - there is no compiled-in SSM path the way Forgejo
has (aligning with [#441](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/441) / [#453](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/453)):

* **`env`** (default, publishable) - read the first non-empty of `WARD_GITHUB_TOKEN`,
  `GH_TOKEN`, `GITHUB_TOKEN`. Zero-config: an external adopter sets one and needs
  nothing else. With none found, the run fails fast naming the three vars.
* **`gh`** - run `gh auth token` on the host at dispatch to mint a fresh token from the
  existing `gh` login, so nothing is pre-exported and a re-warmed session is picked up
  automatically. Fails fast with an actionable error if `gh` is off PATH or logged out.
* **`app`** (follow-up, [ward#534](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/534)) - mint a short-lived, repo-scoped GitHub App
  installation token from an App key (SSM param from operator config, never a baked
  path). The selector arm is wired but returns "not yet implemented" - it is gated on a
  registered GitHub App.

Whichever source resolves it, the token rides the container's private `0600`
`--env-file` as the git-credential channel plus `GH_TOKEN`/`GITHUB_TOKEN` for the
in-container `gh`, unchanged. A classic PAT, a fine-grained PAT, or a GitHub App
installation token all work.
