# Container pre-commit parity

A `ward container` run fresh-clones the target inside the container. A fresh
clone ships **no `.git/hooks`**, so without intervention the agent's `git commit`
(including `ward git commit`) bypasses the repo's pre-commit suite - the same
gate a human's commit runs: lint, comment-cap, doc-layout, trufflehog. Agent
work could land violations straight on `main` that the suite exists to prevent;
nothing flags them until a later host-side commit or a CI run.
[ward#133](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/133).

## The fix

The entrypoint runs `install_precommit_hooks` right after `clone_target` and
before the agent launches, so the hooks are in place for the agent's first
commit. It is the cheapest path to parity and does not rely on the agent
remembering to run the suite by hand.

- Gated on a `.pre-commit-config.yaml` being present in the clone.
- Best-effort: a repo with no config, or an image without `pre-commit` on PATH,
  logs and continues rather than aborting container startup.
- Registers both the default `pre-commit` hook and the `commit-msg` hook.
- Hook *environments* install lazily on the first commit (same as a human's
  first `git commit` after `pre-commit install`), so registration stays cheap
  and offline.

## Interaction with the reaper

The reaper deliberately commits with `--no-verify` (it preserves residual work,
it does not re-gate it; see [container-reap.md](container-reap.md)). So
installing the hooks re-gates **only the agent's own commits** during the
feature, never the salvage backstop. The two concerns stay separate.
