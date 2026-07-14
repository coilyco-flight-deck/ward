---
doc_goal: Document the manual stale reservation cleanup workaround without bloating the operator hub.
---
# manual stale reservation cleanup

Use this when `ward agent list` shows a `container starting` record that has
been marked `cleanup-needed` and does not have a visible running engineer yet.

## Identify it

- Run `ward agent list --json`.
- Look for `phase: container starting`, `status: cleanup-needed`, empty
  `started_at`, and an expired or implausibly old `budget_remaining`.
- Check the issue thread first for terminal evidence such as
  `WARD-OUTCOME: submitted`, `WARD-OUTCOME: merge-ready`, `WARD-OUTCOME:
  blocked`, or `WARD-OUTCOME: failed`.

## Find the comment

```bash
ward ops forgejo issue-comment list <owner> <repo> <issue> --query '[?contains(body, `"ward-agent-reservation"`)].{id:id,created_at:created_at}' --output json
```

## Clear it

```bash
ward ops forgejo issue-comment delete <owner> <repo> <comment-id>
```

- Delete only the targeted reservation comment.
- Never bulk-delete comments.
- Never delete a reservation for a visible or running engineer.
- If an edit verb exists on another surface, editing out the marker is less
  destructive than deleting the whole comment.
- This is issue-comment cleanup, not host-side file deletion.
- After cleanup, run `ward agent list` again and retry dispatch.

## Caveat

Deleting the reservation comment may reduce active count without clearing every
displayed `container starting` record. Host or broker state may still need
[ward#1191](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1191)
and [ward#1196](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1196).
