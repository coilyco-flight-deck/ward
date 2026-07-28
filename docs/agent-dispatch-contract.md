# agent dispatch contract

`ward agent` turns a launch refusal or launch outcome into an exit code and a
tracker comment.

## Exit codes

- `0` - `launched`
- `1` - `launch-failure`
- `2` - `untrusted-owner`
- `3` - `reservation-conflict`
- `4` - `no-go`
- `5` - `wrong-repo`
- `6` - `issue-closed`
- `7` - `mode-ceiling`

## Reap outcomes

- `pushed-to-main`
- `ward-salvage`
- `nothing-to-reap`
- `unknown`

## Contract shape

- trust failures and reservation conflicts are distinct.
- preflight failures are distinct from wrong-repo failures.
- the run outcome is part of the contract, not just the log output.

## See also

- [agent-lifecycle.md](agent-lifecycle.md) - the launch path.
- [agent-ops.md](agent-ops.md) - how failures are read back later.
