---
doc_goal: Show an operator how the guarded execution layer carries a GitHub-hosted issue end to end - clone/push by token, gh-CLI issue thread, PR-as-landing instead of a main push - and make clear this is a real second landing path with its own done-condition, not Forgejo with the labels swapped, while ward's own machinery stays on Forgejo.
---
# ward agent: GitHub as a first-class forge ([ward#489](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/489))

ward is Forgejo-canonical, but GitHub is the public front door. `warded` carries a
GitHub-hosted issue end to end the same way it carries a Forgejo one: it clones and
pushes with a GitHub token, posts the preflight / NO-GO / reservation / outcome
comments on the **GitHub** issue, and the run lands as a **pull request** on GitHub.

## What a GitHub run does differently

- **Clone + push** come off `github.com` with a user-supplied token (below); the git
  credential pushes as `x-access-token`.
- **Issue thread** reads + writes (reservation ping, pre-flight NO-GO, the closing
  retrospective) go through the [`gh` CLI](https://cli.github.com), mirroring how a
  Forgejo run drives `ward ops forgejo`.
- **Landing** is a **pull request**, not a push to `main`. The in-container agent runs
  `gh pr create` whose body carries `Closes #<n>`. The [reaper](container-reap.md) does
  not open the PR on GitHub - it only preserves a salvage branch + files a GitHub issue
  - so the agent opening the PR before it exits is the done-condition.
- **ward's own machinery stays on Forgejo.** The container still downloads the ward
  release + broker binary and warms `/substrate` from Forgejo (`WARD_FORGEJO_BASE`).
  Only the target repo moves to GitHub, via `WARD_CLONE_BASE` + `WARD_FORGE=github`,
  both set automatically from the ref's forge.

## Rate-limit posture

ward's GitHub client keeps reads and state flips on the REST budget, off the
tighter GraphQL lane - see [github-rate-limits.md](github-rate-limits.md).

## Pointing a run at GitHub

* **A github.com URL or short ref** - inferred automatically, no flag:
  ```bash
  warded https://github.com/coilysiren/agentic-os/issues/461
  warded github.com/coilysiren/agentic-os#461
  ```
* **A bare `owner/repo#N` with `--github`** - when the compact ref should mean GitHub:
  ```bash
  warded coilysiren/agentic-os#461 --github
  ```

A plain `owner/repo#N`, a Forgejo URL, or a bare `#N` still mean Forgejo. The
[trusted-owner](agent-trust-gate.md) allowlist is shared across both forges.

## Supplying the GitHub token

GitHub auth is a host-side token, **operator-selectable** by `WARD_GITHUB_TOKEN_SOURCE`
and defaulting to `env` ([ward#533](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/533)): `env` reads `WARD_GITHUB_TOKEN`/`GH_TOKEN`/`GITHUB_TOKEN`,
`gh` mints a fresh one via `gh auth token`, and `app` ([ward#534](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/534)) mints a short-lived,
repo-scoped GitHub App installation token from an App key (SSM param name via operator
config, no baked path). Full detail: [github-token.md](github-token.md).

## Worked example

With `gh` mode ward runs the `gh auth token` call for you - the manual `export` below is
only the `env`-mode equivalent:

```bash
# gh mode: ward mints from your gh login at dispatch, nothing to export
WARD_GITHUB_TOKEN_SOURCE=gh warded https://github.com/coilysiren/agentic-os/issues/461

# env mode (default): export a token yourself (a gh login, or a repo-scoped PAT)
export GITHUB_TOKEN="$(gh auth token)"
warded https://github.com/coilysiren/agentic-os/issues/461
```

ward then, exactly as for Forgejo: runs the pre-flight (a NO-GO comments on the GitHub
issue), posts the reservation comment, spins the ephemeral container that fresh-clones
the repo from GitHub, and detaches. The agent implements on `issue-461`, pushes the
branch, opens a PR closing [#461](https://github.com/coilysiren/agentic-os/issues/461), and leaves its retrospective on the GitHub issue.

## See also

- [agent.md](agent.md) - the `warded` face and role roster.
- [agent-preflight.md](agent-preflight.md) - the GO / NO-GO pre-flight (forge-agnostic).
- [agent-trust-gate.md](agent-trust-gate.md) - the trusted-owner allowlist.
- [container-reap.md](container-reap.md) - the reaper backstop (branch-only on GitHub).
