---
doc_goal: Keep the destination-gated clone rule visible without repeating the full git surface.
---
# ward git clone

`ward git clone` is the guarded clone path.

- The destination is checked before a clone starts.
- The target stays inside the repo authority rules.
- The clone runs through the audited git surface.

## See also

- [git-verbs.md](git-verbs.md) - the full git surface.
- [config-discovery.md](config-discovery.md) - where repo config comes from.

## Destination gate

The destination gate keeps `ward git clone` from silently cloning into the
wrong place.

- a blank or dangerous destination should be refused.
- a safe destination should be explicit.
- the clone should land inside the policy the repo already declared.
