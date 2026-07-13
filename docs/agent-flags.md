---
doc_goal: Give a reader the canonical, code-generated tree of every ward agent command and its direct flags, so the visible flag surface stays aligned with the binary without hand maintenance.
---
# ward agent: the flag tree

<!-- Generated from the code flag tree by `ward agent flags --markdown` (https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1116); do not edit by hand. Regenerate with `make agent-flags`. -->

## `ward agent`

- --harness, --agent, (hidden) --driver, --workflow, (hidden) --branch, --repo, --details, --review-class, --skip-review, --no-review-gate, --github, --config, (hidden) --image, (hidden) --tag, (hidden) --ward-source, (hidden) --ward-version, (hidden) --allow-ward-downgrade, (hidden) --aws, (hidden) --tailnet, (hidden) --tailnet-mode, --print, (hidden) --no-pull, --override-reservation, (hidden) --force, --override-capacity, --skip-preflight, --no-preflight, (hidden) --skip-host-preflight, (hidden) --quiet-seed, --instructions-file, (hidden) --pr

## `ward agent engineer`

- --harness, --agent, (hidden) --driver, --workflow, (hidden) --branch, --repo, --details, --review-class, --skip-review, --no-review-gate, --github, --config, (hidden) --image, (hidden) --tag, (hidden) --ward-source, (hidden) --ward-version, (hidden) --allow-ward-downgrade, (hidden) --aws, (hidden) --tailnet, (hidden) --tailnet-mode, --print, (hidden) --no-pull, --override-reservation, (hidden) --force, --override-capacity, --skip-preflight, --no-preflight, (hidden) --skip-host-preflight, (hidden) --quiet-seed, --instructions-file, (hidden) --pr

## `ward agent director`

- --harness, --agent, (hidden) --driver, --engineer-harness, (hidden) --engineer-driver, --repo, --org, --with-repo, --max-parallel, --triage, --no-triage, --limit, --poll-interval, --max-cycles, --dry-run, (hidden) --image, (hidden) --tag, (hidden) --ward-source, (hidden) --ward-version, (hidden) --allow-ward-downgrade, (hidden) --aws, (hidden) --tailnet, (hidden) --tailnet-mode, --print, --no-pull, --override-reservation, (hidden) --force

## `ward agent director queue`

- --repo, --org, --limit

## `ward agent director merge`

- --repo, --org, --limit, --dry-run, --print

## `ward agent advisor`

- --harness, --agent, (hidden) --driver, --thoroughness, --depth, --repo, --with-repo, --instructions-file, --oneshot, --answer, --config, (hidden) --image, (hidden) --tag, (hidden) --ward-source, (hidden) --ward-version, (hidden) --allow-ward-downgrade, (hidden) --aws, (hidden) --tailnet, (hidden) --tailnet-mode, --no-tailnet, --print, --no-pull

## `ward agent qa`

- --harness, --agent, (hidden) --driver, --thoroughness, --depth, --config, --family, (hidden) --image, (hidden) --tag, (hidden) --ward-source, (hidden) --ward-version, (hidden) --allow-ward-downgrade, (hidden) --aws, (hidden) --tailnet, (hidden) --tailnet-mode, --print, --no-pull

## `ward agent reap`

- --idle, --max-cpu, --interval, --dry-run

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

## `ward agent pr runs`

- --limit

## `ward agent review`

- --class, --diff-base, --ci-log, --worker, --print, --json

