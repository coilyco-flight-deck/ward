---
doc_goal: Explain the split-stack tracker and git authority model for agent launches.
---
# split-stack agent dispatch

Issue tracking and git hosting are separate authorities. A full issue URL pins
the tracker for the whole run, including a brokered launch; Ward does not reduce
it to `owner/repo#N` before forwarding. Checkout remains selected by repository
policy. This supports a Forgejo issue tracker with GitHub as the canonical git
host even when GitHub Issues is disabled.

For compact refs or freeform issue creation, set the tracker beside checkout
policy in the runtime bundle:

```kdl
repo-authority default=forgejo {
    trusted-owner coilysiren
    repo "coilysiren/coilysiren" forge=github tracker=forgejo landing=github mirrors="forgejo"
}
```

`forge` is the checkout host and keeps its legacy behavior. `tracker` selects
the issue API; when omitted it follows `forge` for backward compatibility.
`landing` and `mirrors` describe the intended landing host and known git
mirrors for launch diagnostics. A full tracker URL always wins over `tracker`.

## See also

- [agent-lifecycle.md](agent-lifecycle.md) - the launch path.
