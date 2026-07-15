---
doc_goal: Give a reader the canonical, code-generated tree of every ward agent command and its direct flags, so the visible flag surface stays aligned with the binary without hand maintenance.
---
# ward agent: the flag tree

<!-- Generated from the code flag tree by `ward agent flags --markdown` (https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1116); do not edit by hand. Regenerate with `make agent-flags`. -->

## `ward agent`

- --harness, --agent, --workflow, --branch, --repo, --details, --review-class, --skip-review, --no-review-gate, --github, --config, (hidden) --image, (hidden) --tag, (hidden) --ward-source, (hidden) --ward-version, (hidden) --allow-ward-downgrade, --print, (hidden) --no-pull, --override-reservation, --override-capacity, --skip-preflight, --no-preflight, (hidden) --skip-host-preflight, (hidden) --quiet-seed, --instructions-file, (hidden) --pr
## `ward agent engineer`

- --harness, --agent, --workflow, --branch, --repo, --details, --review-class, --skip-review, --no-review-gate, --github, --config, (hidden) --image, (hidden) --tag, (hidden) --ward-source, (hidden) --ward-version, (hidden) --allow-ward-downgrade, --print, (hidden) --no-pull, --override-reservation, --override-capacity, --skip-preflight, --no-preflight, (hidden) --skip-host-preflight, (hidden) --quiet-seed, --instructions-file, (hidden) --pr
## `ward agent director`

- --harness, --agent, --engineer-harness, --repo, --org, --with-repo, --max-parallel, --burndown, --drain, --triage, --no-triage, --limit, --poll-interval, --max-cycles, --dry-run, (hidden) --image, (hidden) --tag, (hidden) --ward-source, (hidden) --ward-version, (hidden) --allow-ward-downgrade, --print, --no-pull, --override-reservation
## `ward agent director queue`

- --repo, --org, --limit
## `ward agent director merge`

- --repo, --org, --limit, --dry-run, --print
## `ward agent qa`

- --harness, --agent, --thoroughness, --depth, --config, --family, (hidden) --image, (hidden) --tag, (hidden) --ward-source, (hidden) --ward-version, (hidden) --allow-ward-downgrade, --print, --no-pull
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

- --tail, --follow
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
## `ward agent review`

- --class, --diff-base, --ci-log, --worker, --print, --json
