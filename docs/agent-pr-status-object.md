---
doc_goal: Describe the agent-shaped PR/CI status object, wait primitive, and log follow-up surface.
---
# ward agent pr status object

`ward agent pr status`, `ward agent pr wait`, and `ward agent pr logs` share one compact PR/CI snapshot. It carries the PR number, head SHA, combined and required status, failing and pending contexts, latest run ids, log hooks, repair hints, and the next recommended action.

## Shape

- `pr` - PR number, title, URL, state, draft bit, mergeable bit.
- `base` - base ref and required contexts when branch protection exposes them.
- `head` - current head ref and SHA.
- `status` - combined status, required status, terminal bit, observed time, optional pinned head, and head mismatch.
- `contexts` - per-context state with run/job/log hook joins when available.
- `failing_contexts` / `pending_contexts` - compact lists for wait and text output.
- `latest_runs` - Actions runs filtered to the current head SHA.
- `log_hooks` - direct follow-up hooks for `ci.log.read`.
- `repair` - current repair bucket and note when Ward can classify one.
- `next_action` - `wait`, `fetch_logs`, `repair_pr`, `rebase_or_refresh`, `merge`, `blocked`, or `none`.

## Wait

`ward agent pr wait <owner/repo#N> [--timeout D] [--interval D] [--head SHA] [--json]`

- `0` when the required status is green.
- `1` on terminal red states or a head mismatch.
- `124` on timeout.
- `2` on usage or auth failure.

## Logs

`ward agent pr logs <owner/repo#N> [--context NAME]` uses the same snapshot to jump to the matching CI log stream. It keeps the run id, job name, and attempt inside the object so agents do not have to rebuild the PR -> commit -> run -> job mapping by hand.

## Examples

Engineer handoff:

```bash
ward agent pr status coilyco-flight-deck/ward#123 --json
ward agent pr wait coilyco-flight-deck/ward#123 --timeout 30m --interval 15s
ward agent pr logs coilyco-flight-deck/ward#123 --context test
```

Director merge handoff:

```bash
ward agent pr status coilyco-flight-deck/ward#123
ward agent pr wait coilyco-flight-deck/ward#123
ward agent director merge coilyco-flight-deck/ward#123
```
