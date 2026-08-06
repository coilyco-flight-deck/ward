---
doc_goal: Explain Ward's supported stale-launch and disposable-cache cleanup paths.
---
# stale reservation cleanup

Ward keeps issue-thread reservation authority behind typed broker operations.
It does not expose a generic raw comment-deletion command.

Use this when `ward agent list` shows a `container starting` record that still
counts toward capacity but does not have a visible running engineer yet.

## Identify it

- Run `ward agent list --json`.
- Look for `phase: container starting`, `status: cleanup-needed`, empty
  `started_at`, and an expired or implausibly old `budget_remaining`.
- Check the issue thread first for terminal evidence such as
  `WARD-WORKFLOW: submitted`, `WARD-WORKFLOW: merge-ready`,
  `WARD-WORKFLOW: blocked`, or `WARD-WORKFLOW: failed`. Older
  `WARD-OUTCOME:` comments remain readable as compatibility input.

## Clear the stale launch

```bash
ward agent stop <owner/repo#N> --print
ward agent stop <owner/repo#N>
```

The preview names the run or stale issue-ref launch Ward will target. The real
command routes through the supervised broker and clears only the confirmed
Ward-owned state.

If the issue thread is already correct and only the disposable host cache is
stale, clear that cache wholesale:

```bash
ward agent reservations clear
```

- Never clear a reservation for a visible or running engineer.
- The cache command does not delete canonical issue-thread evidence.
- After cleanup, run `ward agent list` again and retry dispatch.

## Caveat

Deleting the reservation comment may reduce active count without clearing every
displayed `container starting` record. Host or broker state may still need
[ward#1191](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1191)
and [ward#1196](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1196).
