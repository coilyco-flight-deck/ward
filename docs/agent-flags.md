---
doc_goal: Give a reader the canonical code-generated flags for Ward's selected operational agent command paths, deliberately omitting self-description and fixed PR or issue leaves documented by their parent contracts.
---
# ward agent: the flag tree

<!-- Generated from the code flag tree by `ward agent flags --markdown` (https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1116); do not edit by hand. Regenerate with `make agent-flags`. -->

## `ward agent`

- --harness, --agent, --workflow, --branch, --repo, --details, --review-class, --skip-review, --no-review-gate, --github, --config, (hidden) --image, (hidden) --tag, --context-bundle, (hidden) --ward-source, (hidden) --ward-version, (hidden) --allow-ward-downgrade, --print, (hidden) --no-pull, --override-reservation, --override-capacity, --skip-preflight, --no-preflight, --skip-smoke-test, (hidden) --skip-host-preflight, (hidden) --quiet-seed, --instructions-file, (hidden) --pr
## `ward agent engineer`

- --harness, --agent, --workflow, --branch, --repo, --details, --review-class, --skip-review, --no-review-gate, --github, --config, (hidden) --image, (hidden) --tag, --context-bundle, (hidden) --ward-source, (hidden) --ward-version, (hidden) --allow-ward-downgrade, --print, (hidden) --no-pull, --override-reservation, --override-capacity, --skip-preflight, --no-preflight, --skip-smoke-test, (hidden) --skip-host-preflight, (hidden) --quiet-seed, --instructions-file, (hidden) --pr
## `ward agent director`

- --harness, --agent, --repo, --org, --with-repo, --limit, (hidden) --image, (hidden) --tag, --context-bundle, (hidden) --ward-source, (hidden) --ward-version, (hidden) --allow-ward-downgrade, --print, --no-pull
## `ward agent director queue`

- --repo, --org, --limit, --json
## `ward agent director merge`

- --repo, --org, --limit, --dry-run, --print
## `ward agent qa`

- --harness, --agent, --thoroughness, --depth, --config, --family, (hidden) --image, (hidden) --tag, --context-bundle, (hidden) --ward-source, (hidden) --ward-version, (hidden) --allow-ward-downgrade, --print, --no-pull
## `ward agent run`

- --harness, --agent, --role, (hidden) --agent-id, --cluster, --repo, --config, (hidden) --image, (hidden) --tag, --context-bundle, (hidden) --ward-source, (hidden) --ward-version, (hidden) --allow-ward-downgrade, --print, (hidden) --no-pull
## `ward agent cluster`

- No direct flags.
## `ward agent cluster start`

- --harness, --agent, (hidden) --image, (hidden) --tag, --context-bundle, (hidden) --ward-source, (hidden) --ward-version, (hidden) --allow-ward-downgrade, --print
## `ward agent cluster list`

- --json
## `ward agent cluster status`

- --json
## `ward agent cluster logs`

- --tail
## `ward agent cluster stop`

- --print
## `ward agent message`

- No direct flags.
## `ward agent message send`

- --to, --conversation
## `ward agent message receive`

- --after, --conversation, --json
## `ward agent approval-plan`

- --pr, --comment-id
## `ward agent reap`

- --idle, --max-cpu, --interval, --dry-run
## `ward agent reservations`

- No direct flags.
## `ward agent reservations clear`

- No direct flags.
## `ward agent stop`

- --print
## `ward agent list`

- --json
## `ward agent logs`

- --tail, --follow, --artifact
## `ward agent dispatch`

- No direct flags.
## `ward agent dispatch list`

- --json
## `ward agent dispatch status`

- --json
## `ward agent dispatch prune`

- --older-than, --confirm, --json
## `ward agent issue create`

- --title, --body-file
## `ward agent issue approve`

- --pr, --intent-comment-id
## `ward agent dispatch-health`

- --repo, --org, --limit, --max-parallel, --json, --line
## `ward agent pr`

- No direct flags.
## `ward agent pr wait`

- --timeout, --interval, --head, --json
## `ward agent pr logs`

- --context
## `ward agent pr close`

- --reason, --supersedes
## `ward agent pr reopen`

- No direct flags.
## `ward agent pr recover`

- No direct flags.
## `ward agent pr runs`

- --limit
## `ward agent recover`

- --apply, --work, --include-quarantined, --override-reservation
## `ward agent recover prune`

- --older-than, --confirm
## `ward agent review`

- --class, --diff-base, --ci-log, --worker, --print, --json
