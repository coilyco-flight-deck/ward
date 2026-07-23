---
doc_goal: Capture the human-feedback gate in one small page so the PR workflow docs stay under the size cap.
---
# ward agent human feedback

Ward treats structured `WARD-WORKFLOW:` comment-family markers as automation by default. The former `WARDED_WORKFLOW:` and older typed `WARD-*` headers are recognized for historical threads. Plain comments from the repo owner, the configured push user, or any other author block close/reopen/merge/done until they are visibly acknowledged by a newer ward-authored comment.

The host config extension point lives under `~/.ward/config.yaml`:

```yaml
agent:
  human-feedback:
    ignore-authors:
      - helper-bot
    automation-markers:
      - custom-automation:
```

- `ignore-authors` starts empty.
- `automation-markers` is optional and extends the built-in ward marker family.
- Forgejo review records are not surfaced by the current client, so the gate uses comment threads plus available issue/PR update timestamps and fails closed on the data it can see.

## See also

- [agent-pr-workflow.md](agent-pr-workflow.md) - the PR workflow verbs that use the gate.
