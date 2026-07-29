---
doc_goal: Define Ward's three fixed workflow labels without implying authority.
---
# Ward agent roles

`ward agent` exposes three fixed workflow labels:

- `engineer` implements one issue in a detached container. Its execution limit
  is 90 minutes.
- `director` opens the attached, read-only supervisory surface. It has no
  execution limit.
- `qa` inspects a candidate and posts a structured verdict. Its execution limit
  is 30 minutes.

These labels select workflow behavior and reporting only. A label cannot alter
credentials, mounts, network access, broker grants, merge authority, model,
identity, or container topology. Ward enforces those boundaries in the fixed
launch and broker paths.

`--verification-fixture` narrows director, engineer, and QA runs to an
explicitly configured disposable target. It does not change the role's
credentials or authority. See [verification-fixtures.md](verification-fixtures.md).

`warded #98` selects `engineer`. `warded director --repo owner/name` selects
`director`. `warded qa #98` selects `qa`. Ward rejects additional startup role
names instead of loading a profile.

See [agent.md](agent.md), [agent-director.md](agent-director.md), and
[agent-lifecycle.md](agent-lifecycle.md).
