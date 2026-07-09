---
doc_goal: Make the demo land ward's thesis that the boundary is the product - one beat on capability, two on denial, and one host posture check - while being scrupulously honest about which mechanism refuses each danger class, so the denial shown is the one that actually holds.
---
# The demo: one happy path, two denials, one posture check

The launch thesis is **the boundary is the product** ([ward#229](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/229)): what matters
about ward is not what it runs, it is what it **refuses**. So the demo spends one
beat on capability, two on denial, and one on host posture. [`examples/demo.sh`](../examples/demo.sh)
is the runnable script ([ward#251](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/251)). This page is the walkthrough: what each
beat proves and **which mechanism refuses**, so the denial you show is the one
that holds.

## Run it

```sh
sh examples/demo.sh          # or: cd examples/toy && sh ../demo.sh
```

It drives [`examples/toy/`](../examples/toy/README.md), ward's minimal managed
repo, so it needs no toolchain. It is safe: Beat 1 runs the toy's real test
verb, Beat 2 is rejected before the command executes, Beat 3 checks host
security posture with `ward doctor`, and Beat 4 is refused by policy before any
endpoint or token is touched. Run it from a clean, pushed checkout for the
green happy path.

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

## Beat 3 - host posture check

The toy repo's `security:` block names `kubectl` and `aws` as protected, and
`ward doctor` surfaces that posture together with the sudo probe. This beat is
diagnosis, not denial. It is the control panel that tells an operator what the
policy says before a human or agent reaches for the bare binary.

```
$ ward doctor --skip ollama
```

The hard denials backing the thesis are Beat 2's verb gate and Beat 4's
operator surface. Beat 3 is the safety read, not a host-side hook.

## Beat 4 - danger class three: ops danger (the operator surface)

The class ward exists for ([ward#250](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/250)) - a headless devsecops agent that **holds
live credentials** reaching for an operator verb that is out of policy:

```
$ ward ops forgejo pr list coilyco-flight-deck ward
ward: pull requests are not exposed through ward; read them in the web UI
[exit 2]
```

This is the **hard sibling of Beat 3**. Not a fail-open hint on a bare binary,
but the compiled [ward-kdl](ward-kdl.md) operator surface refusing the verb
itself. ward's policy withholds pull-request reads (they go through the web UI),
so `ward ops forgejo pr` answers `denied by policy` **before any endpoint or
token is touched** - the reason it runs credential-free in the demo. It is a real
denial, not an absence: the verb is present on the surface and refuses.

The truly destructive mutations are withheld one step further, absent at compile
time rather than denied at runtime ([ward-kdl.md](ward-kdl.md)): `ward ops aws
s3` exposes only `ls`/`cp`/`sync` (no `rm`), and `ward ops kubectl` has no
`delete`, so an agent cannot reach them through the gate at all. The **allowed**
ops verbs (`ward ops forgejo issue get`, `ward ops kubectl get`) run audited,
exactly like Beat 1 - so the audit log carries **both** the allow and the deny.
This is the boundary a filesystem-plus-git sandbox cannot draw, because the
danger here is **ops-adjacent** ([ward#229](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/229) positioning), and it is the one
denial in the demo that is both **hard** and a **real ops verb**.

## See also

- [gate-demo.md](gate-demo.md) - the two hard-gate refusals in prose.
- [example-repo.md](example-repo.md) - the `examples/toy/` repo the demo drives.
- [enforcement-boundary.md](enforcement-boundary.md) - which mechanism holds, per harness.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
