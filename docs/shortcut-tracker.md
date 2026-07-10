---
doc_goal: Explain ward's Shortcut tracker adapter: how it reads and writes Stories, how state transitions map to workflow states, and how the Shortcut story URL round-trips through ward's issue-ref parser.
---
# Shortcut tracker adapter

ward treats a Shortcut Story as the tracker-side issue thread for split-stack runs.

## Auth

- Set `SHORTCUT_API_TOKEN` to a user API token.
- ward sends it as the `Shortcut-Token` header to `https://api.app.shortcut.com/api/v3`.

## What the adapter does

- Reads a Story with `GET /stories/{id}`.
- Lists and posts comments with `/stories/{id}/comments`.
- Closes a Story by moving it to the workflow's `done` state.
- Reopens a Story by moving it back to the workflow's default state.
- Creates a Story in the default workflow state when ward needs a new tracker issue.
- If `SHORTCUT_LABELS` is set, splits that comma list into story labels.
- If `SHORTCUT_EPIC_ID` is set, attaches that epic to the created Story.

## Ref shape

- Shortcut story URLs parse from `https://app.shortcut.com/<workspace>/story/<id>/<slug>`.
- ward preserves that URL when it sees one and uses it again when rendering the carried issue.
- Short `sc-123` style refs are also accepted for the story number.

## Notes

- The adapter resolves state transitions from the workspace's workflow list.
- If a workspace needs a different creation state, set `SHORTCUT_WORKFLOW_STATE_ID`.
- For create-time metadata, set `SHORTCUT_LABELS` and `SHORTCUT_EPIC_ID`.

## See also

- [compat-surface.md](compat-surface.md)
- [FEATURES.md](FEATURES.md)
