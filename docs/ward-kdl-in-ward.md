---
doc_goal: Keep the exec-dialect auto-mount rule in one concise place after the generated reference docs were removed.
---
# ward-kdl in ward

Exec-dialect guardfiles can mount into `ward` at their `wrap` path.

- `ward-kdl docker` becomes `ward docker`.
- `ward-kdl ops aws` becomes `ward ops aws`.
- `ward-kdl agents claude` becomes `ward agents claude`.

Hand-written surfaces still win collisions.

## Why it matters

- it removes the one-off Go graft pattern.
- it keeps exec guardfiles on the same audited path as the rest of ward.
- it lets the generated surface show up under the shipped binary without a
  second command tree.
- it accepts `first input` as sugar for `arg0` in exec guardfile predicates, so
  older and newer guardfiles can share the same mount path.

## Collision rule

If `ward` already owns a path, the hand-written command stays in place and the
guardfile mount skips that leaf. That keeps the special cases where they belong
and prevents the generated surface from silently overriding a deliberate
hand-written command.

## See also

- [ward-kdl-surface.md](ward-kdl-surface.md) - the generated family list.
