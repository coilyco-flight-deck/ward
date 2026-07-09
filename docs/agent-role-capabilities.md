---
doc_goal: Explain ward's semantic role-capability vocabulary - read, project-management, engineering, ops, and admin - and the rule that named roles are presets over that vocabulary while ward-kdl keeps owning the guarded edge surfaces.
---
# ward agent: semantic role capabilities

Ward owns a small semantic capability vocabulary for agent roles:

- `read`
- `project-management`
- `engineering`
- `ops`
- `admin`

These names describe role posture, not KDL edge surfaces. `ward-kdl` still owns
the guarded command surfaces and permission tiers. Ward owns the role behavior
layer and maps named startup roles onto semantic presets.

## Default presets

- `advisor` - `read`
- `qa` - `read`
- `director` - `read + project-management`
- `engineer` - `read + engineering`
- `ops` - `read + ops` when that startup role lands
- `admin` - a human/operator aggregate special case, not part of the normal agent baseline

Named roles are presets, not the only possible model. Future role shapes can be
composed from the same vocabulary without changing the KDL edge-surface layer.

## Boundary

Do not confuse these names with the KDL surface tiers in [ward-kdl-surface.md](ward-kdl-surface.md).
The `read` / `write` / `admin` binaries there are guarded edge surfaces, not
ward role postures.

## See also

- [agent.md](agent.md) - the startup roles that carry these presets.
- [agent-capability.md](agent-capability.md) - the separate host/cloud reach model.
- [ward-kdl.md](ward-kdl.md) - the build-time layer that owns the edge surfaces.

