---
name: tooling-ward-agent
description: Normalize one dictated Ward issue reference and dispatch its detached engineer through the current governed agent surface.
---

# Dispatch one Ward engineer

Use this skill when Kai asks to dispatch, spawn, fire, or fan out one already
filed issue. It resolves one exact issue and launches one detached engineer. It
does not slice work, author issue bodies, run an autonomous queue, or open an
interactive engineer.

## Resolve the reference

Accept an explicit `owner/repo#N`, issue URL, or a bare `#N` inside the target
checkout. For dictated aliases, apply only the collision rules in
[`references/normalization.md`](references/normalization.md). If they do not
produce one exact repository, ask for `owner/repo` rather than guessing or
querying an external inventory.

Read the issue title and state through Ward's director queue or the current
harness's tracker read surface. Refuse a closed issue, unresolved repository,
untrusted owner, or failed lookup. A unique explicit match needs no second
confirmation when Kai already asked to dispatch it.

## Dispatch

Run the detached engineer:

```bash
ward agent engineer owner/repo#123
```

`--harness claude|codex|goose|opencode` selects a typed harness when Kai names
one. A bare ref without the role word selects the same engineer behavior.
Return after Ward accepts or refuses the launch. The returning command is not
proof the run finished. Use `ward agent dispatch status`, `ward agent list`,
and `ward agent logs` for later observation.

Interactive supervision is the attached read-only director:

```bash
ward agent director --repo owner/repo
```

The director reads live state and exposes governed primitives. It does not run
an autonomous polling or prioritization loop.

## Boundaries

Ward owns trust, reservation, capacity, preflight, credentials, clone,
container, workflow, audit, and teardown. Do not bypass those checks in this
skill. Use [`tooling-warded-execution-model`](../tooling-warded-execution-model/SKILL.md)
for lifecycle or ownership questions.
