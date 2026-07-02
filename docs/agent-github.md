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

GitHub auth is a **user-supplied token from the environment** - there is no compiled-in
SSM path the way Forgejo has (aligning with [#441](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/441) / [#453](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/453)). ward reads the first of these
it finds, on the host at dispatch and again inside the container:

1. `WARD_GITHUB_TOKEN`
2. `GH_TOKEN`
3. `GITHUB_TOKEN`

The token needs `repo` scope (clone, push, open a PR) plus issue read/write; a classic
PAT, a fine-grained PAT, or a GitHub App installation token all work. It rides the
container's private `0600` `--env-file` (never on argv, never in the audit log) as the
git-credential channel plus `GH_TOKEN`/`GITHUB_TOKEN` for `gh`. With no token found, the
run fails fast at dispatch naming the three env vars - it never falls through to SSM.

## Worked example

```bash
export GITHUB_TOKEN="$(gh auth token)"     # reuse a gh login, or paste a repo-scoped PAT
warded https://github.com/coilysiren/agentic-os/issues/461
```

ward then, exactly as for Forgejo: runs the pre-flight (a NO-GO comments on the GitHub
issue), posts the reservation comment, spins the ephemeral container that fresh-clones
the repo from GitHub, and detaches. The agent implements on `issue-461`, pushes the
branch, opens a PR closing [#461](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/461), and leaves its retrospective on the GitHub issue.

## See also

- [agent.md](agent.md) - the `warded` face and role roster.
- [agent-preflight.md](agent-preflight.md) - the GO / NO-GO pre-flight (forge-agnostic).
- [agent-trust-gate.md](agent-trust-gate.md) - the trusted-owner allowlist.
- [container-reap.md](container-reap.md) - the reaper backstop (branch-only on GitHub).
