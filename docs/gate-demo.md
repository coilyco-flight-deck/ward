# The gate says no

ward is a security boundary, so the interesting demo is not what it runs - it is
what it **refuses**. The clean-tree gate declines a verb when the run could not
be reconstructed from history, and the argv policy declines anything carrying
shell metacharacters:

```
$ ward exec test                 # on a branch with no upstream set
ward exec test: refused - repo verb gated on a clean, synced tree
  reason: HEAD has no synced upstream (push or set upstream first)
  the audit row must be reconstructable from committed history
  override for a genuine emergency: ward --audit-override-dirty exec test

$ ward exec test -- -run 'Foo; rm -rf /'
ward exec test: refused - argument "Foo; rm -rf /" contains a shell metacharacter
```

The override exists, but it is loud: the audit row is stamped `audit_override=true`
with the full working-tree status, so an emergency bypass is still reconstructable
after the fact. Denial is the default posture, not an error path.

This is the danger-class demo the launch doctrine ([ward#255](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/255), "every demo must
show a denial") and the [ward#251](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/251) demo script want - it lives here, in the demo
surface, rather than on the README front page (the 2026-07-01 triage on [ward#444](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/444)
declined a front-page denial demo).

## See also

- [exec-verb.md](exec-verb.md) - the `ward exec` gate these refusals come from (clean-tree gate + override mechanics).
- [agent-gate.md](agent-gate.md) - the interactive pre-launch gate.
- [../README.md](../README.md) - human-facing intro.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
