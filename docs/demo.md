# The demo: one happy path, two danger classes

The launch thesis is **the boundary is the product** ([ward#229](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/229)): what matters
about ward is not what it runs, it is what it **refuses**. So the demo spends one
beat on capability and two on denial. [`examples/demo.sh`](../examples/demo.sh)
is the runnable script ([ward#251](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/251)). This page is the walkthrough: what each
beat proves and **which mechanism refuses**, so the denial you show is the one
that holds.

## Run it

```sh
sh examples/demo.sh          # or: cd examples/toy && sh ../demo.sh
```

It drives [`examples/toy/`](../examples/toy/README.md), ward's minimal managed
repo, so it needs no toolchain. It is safe: Beat 1 runs the toy's real test
verb, Beat 2 is rejected before the command executes, and Beat 3 only feeds a
string to `ward hook pre-tool-use` (inspect-and-refuse, nothing runs). Run it
from a clean, pushed checkout for the green happy path.

## Beat 1 - happy path: `ward exec test`

`ward exec test` runs the toy's `make test` through cli-guard: every argv token
validated, one JSONL row appended to `~/.ward/audit/<repo>.jsonl`, the run gated
on a clean, synced tree so that row reconstructs from git history. The demo tails
the row it just wrote, so the audit is shown, not asserted. The real run
references a Forgejo issue (`fj`). The one-liner an adopter copies uses `gh` -
same gate, either forge. See [exec-verb.md](exec-verb.md), [audit.md](audit.md).

## Beat 2 - danger class one: repo danger (the hard gate)

A destructive argument smuggled into a benign-looking test filter:

```
$ ward exec test -- -run 'Cleanup; rm -rf $HOME'
ward: policy: shell metacharacter rejected: arg positional[3] contains ';' at index 7
```

The `gh pr merge` / force-push family of repo damage, here as shell injection
into a real gated verb. This is the **hard boundary** - the compiled cli-guard
verb pipeline every `ward exec` runs through, not a hint - and it holds for every
harness in the `ward agent` flow. Its sibling is the clean-tree refusal (an
unsynced branch is declined the same way). Both bypass **loudly**:
`ward --audit-override-dirty` stamps the row `audit_override=true`. Deep dive:
[gate-demo.md](gate-demo.md), [exec-verb.md](exec-verb.md).

## Beat 3 - danger class two: infra danger (protected binary)

The bounded-autonomy money shot - an agent reaching for infra it should never
touch bare. The toy's `security:` block names `kubectl` and `aws` as protected:

```
$ echo <kubectl delete deployment checkout-api> | ward hook pre-tool-use
ward hook: blocked protected binary `kubectl`. Direct invocation is denied. Recovery: route kubectl through a ward-kdl wrapper, not the bare binary
$ echo <aws s3 rm s3://prod-uploads --recursive> | ward hook pre-tool-use
ward hook: blocked protected binary `aws`. Direct invocation is denied. Recovery: route aws through a ward-kdl wrapper, not the bare binary
```

**Be honest about this one.** This beat is the Claude PreToolUse hook, a
host-side, **claude-only, fail-open hint** ([hook.md](hook.md),
[enforcement-boundary.md](enforcement-boundary.md)) - the nudge, not the wall.
The hard denials backing the thesis are Beat 2's verb gate and, in the `ward
agent` flow, the container edge plus cli-guard. Name the mechanism when you show
the denial.

Swapping a danger verb is a one-line edit to the toy `security:` block
([ward-yaml.md](ward-yaml.md)) plus the matching script line - but keep it to a
verb ward actually refuses, or the demo proves nothing.

## See also

- [gate-demo.md](gate-demo.md) - the two hard-gate refusals in prose.
- [example-repo.md](example-repo.md) - the `examples/toy/` repo the demo drives.
- [enforcement-boundary.md](enforcement-boundary.md) - which mechanism holds, per harness.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
