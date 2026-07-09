---
doc_goal: Explain the opt-in QA inspection role as a structured, read-only verdict surface - how it inspects the candidate issue, branch or PR, and checks without editing implementation state, and how its failure stays advisory in this first slice.
---
# ward agent qa

`ward agent qa` (public face `warded qa`) is the **opt-in QA inspection** role of
the startup roster. It is read-only and comment-oriented: a ref inspects the issue,
the candidate branch or pull request, and the checks, then posts a structured QA
verdict comment.

## Usage

```bash
warded qa coilyco-flight-deck/ward#98                      # ref -> structured QA verdict comment
warded qa coilyco-flight-deck/ward#98 "check the PR too"   # ref with extra framing
```

## Argument-type dispatch

`parseAgentIssueRef` succeeds -> ref mode. The role is not a freeform session and
does not expose an interactive attach surface.

## Ref mode: inspect + comment

The QA run fetches the issue and comment thread, seeds a read-only inspection
container, and asks for a single structured verdict. The output is posted back on
the issue as a durable comment.

The verdict is advisory in this slice. A failing QA verdict is recorded and surfaced,
but it does not gate landing or change implementation state. That makes the role
opt-in only until the later brokered gating design lands deliberately.

## See also

- [docs/agent.md](agent.md) - the roster and the `warded` face.
- [docs/agent-subcommands.md](agent-subcommands.md) - the roles compared.
