---
doc_goal: Show an operator how ward carries a GitLab-hosted issue end to end - clone/push by token, native GitLab issue comments, and merge-request landing instead of a GitHub-style PR - while keeping the git-host and issue-thread concerns explicit and self-hosted GitLab configurable.
---
# ward agent: GitLab as a first-class forge

ward is Forgejo-canonical, but GitLab is a first-class foreign forge. `warded`
carries a GitLab-hosted issue end to end the same way it carries a GitHub one: it
clones and pushes with a GitLab token, posts the preflight / NO-GO / reservation /
outcome comments on the **GitLab issue**, and the run lands as a **merge request**.

## What a GitLab run does differently

- **Clone + push** come off the configured GitLab base URL with a user-supplied
  token. The git credential pushes as `oauth2`.
- **Issue thread** reads + writes go through ward's GitLab client, matching GitLab's
  native issues instead of splitting the tracker surface away from the forge.
- **Landing** is a **merge request**, not a push to `main`. The in-container agent
  should open an MR whose body carries `closes #<n>`. After the MR opens, the agent
  keeps watching its checks and only reports done once they are green or the run is
  genuinely blocked.
- **Self-hosted GitLab is configurable.** `WARD_GITLAB_BASE` sets the GitLab base URL
  for both issue URLs and clone/push targets. The default is `https://gitlab.com`.

## Supplying the GitLab token

GitLab auth is host-side token resolution plus a CLI fallback when no env token is
set. ward first reads `WARD_GITLAB_TOKEN`, `GITLAB_TOKEN`, `GITLAB_ACCESS_TOKEN`, or
`OAUTH_TOKEN`. If none are present and `glab` is on PATH, ward shells `glab auth
token` for a host-side fallback. Use `WARD_GITLAB_TOKEN_SOURCE=glab` to force the CLI
fallback.

## Pointing a run at GitLab

GitLab issue URLs are recognized directly when the configured base matches the host:

```bash
warded https://gitlab.example.com/group/proj/-/issues/12
warded https://gitlab.example.com/group/proj/-/merge_requests/34
```

Set `WARD_GITLAB_BASE` before launch when the instance is self-hosted:

```bash
export WARD_GITLAB_BASE=https://gitlab.example.com
warded https://gitlab.example.com/group/proj/-/issues/12
```

## See also

- [agent-github.md](agent-github.md) - the GitHub front-door twin.
- [container-env.md](container-env.md) - the `WARD_*` launch env contract.
- [compat-surface.md](compat-surface.md) - the forge/tracker seam map.
