# Agent-only commit suite

`ward agent headless` and `ward agent task` run the in-container
agent non-interactively, end to end, to a clean `main` push. With no human in
the loop, those runs can hold commits to a bar a human would find too annoying
live - exactly the checks
[ward#139](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/139)
calls for: a same-repo issue close and conventional-commit subjects.

## Reuse, not reinvention

The [agentic-os](https://forgejo.coilysiren.me/coilyco-flight-deck/agentic-os)
pre-commit suite already ships those two hooks - `closes-issue` and
`conventional-commit` - both off by default (a repo opts in per hook). ward does
not reimplement them; it turns them on for agent runs only.

- **`closes-issue`** requires a same-repo Forgejo issue **URL**
  (`closes https://forgejo.coilysiren.me/<owner>/<repo>/issues/N`). It rejects
  bare `#N` / `owner/repo#N`, which GitHub auto-closes by accident on a mirrored
  repo. The full URL is the kind of thing a human hates typing and an agent does
  for free.
- **`conventional-commit`** enforces a Conventional Commits 1.0.0 subject.

## How it is wired

`install_agent_precommit_hooks` in the entrypoint runs **only when
`WARD_HEADLESS=1`** (set by `headless` and `task`, never by interactive `work`
or on a host), right after the clone and the repo's own
[pre-commit parity install](container-precommit.md):

1. `ward container agent-precommit-config` reads the clone's
   `.pre-commit-config.yaml` and emits an agent-only config that pins the **same
   agentic-os rev the repo already uses** (so pre-commit reuses the cached env)
   with just `closes-issue` + `conventional-commit`. No agentic-os entry → it
   skips, leaving the commit untouched.
2. `pre-commit install --hook-type commit-msg --config <that file>` binds it as
   the clone's `commit-msg` hook.

The agent suite owns the `commit-msg` hook for the run; the repo's pre-commit
stage hooks (`.git/hooks/pre-commit`) are untouched.

## Interaction with the reaper

The reaper commits with `--no-verify` by design (it preserves residual work, it
does not re-gate it; see [container-reap.md](container-reap.md)). So the suite
gates **only the agent's own commits** during the feature, never the salvage
backstop - the same separation the [pre-commit parity](container-precommit.md)
fix keeps.
