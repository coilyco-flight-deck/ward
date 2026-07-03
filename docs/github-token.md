---
doc_goal: Let an operator pick and provision the right GitHub token source for a warded GitHub-hosted run - env, gh, or short-lived repo-scoped App - understanding that resolution stays host-side and env-only with no baked SSM path, and how the minted App token bounds a leak to one repo for minutes.
---
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
* **`app`** ([ward#534](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/534)) - mint a short-lived, repo-scoped GitHub App installation token at
  dispatch. Two env vars carry the operator config: `WARD_GITHUB_APP_ID` (the numeric App
  ID) and `WARD_GITHUB_APP_KEY_SSM` (the **name** of the SSM param holding the App's PEM
  private key). ward fetches that key from SSM through its audited `aws` runner, signs an
  RS256 App JWT (backdated `iat`, 9-minute `exp`), resolves the target repo's installation,
  and exchanges the JWT for a token scoped to just that one repo. The SSM param name is
  operator config on an env var, never a baked Go literal or path, so ward stays env-only
  in-tree (aligning with [#441](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/441) / [#453](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/453)). The minted token is ephemeral and carries the
  App's bot identity, so it is leak-resilient by construction ([infra#441](https://github.com/coilysiren/infrastructure/issues/441)). Using `app`
  needs a registered GitHub App installed on the target repos - with the env unset, the run
  fails fast naming both vars.

Whichever source resolves it, the token rides the container's private `0600`
`--env-file` as the git-credential channel plus `GH_TOKEN`/`GITHUB_TOKEN` for the
in-container `gh`, unchanged. A classic PAT, a fine-grained PAT, or a GitHub App
installation token all work.

## App mode: what the operator provisions

`app` mode reads two env vars host-side at dispatch, both operator config:

* `WARD_GITHUB_APP_ID` - the App's numeric ID, the JWT `iss`.
* `WARD_GITHUB_APP_KEY_SSM` - the SSM param **name** (e.g. `/ward/github-app/private-key`)
  whose SecureString value is the App's PEM private key (PKCS1 or PKCS8 both parse).

```bash
export WARD_GITHUB_TOKEN_SOURCE=app
export WARD_GITHUB_APP_ID=123456
export WARD_GITHUB_APP_KEY_SSM=/ward/github-app/private-key
warded https://github.com/coilysiren/agentic-os/issues/461
```

The key value only leaves SSM at dispatch and never touches argv or the audit row; the
resulting installation token is scoped to the single target repo and expires within the
hour, so a leak is bounded to one repo for minutes.
