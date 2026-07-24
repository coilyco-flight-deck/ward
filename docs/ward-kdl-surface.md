---
doc_goal: Record the Aguard operator surface without implying Ward embeds it.
---
# Aguard operator surface

Generated operator APIs moved out of Ward. The current AOS image provides:

- `aguard ops forgejo`
- `aguard ops actions`
- `aguard ops aws`
- `aguard ops kubectl`
- `aguard ops tailscale`

Specgen builds these leaves during the AOS image build. Aguard is standalone at
runtime: its help and execution do not invoke, brand, or configure Ward.

Ward has no generated operator command family and no runtime guardfile mount.

## See also

- [ward-kdl.md](ward-kdl.md) - the boundary.
- [ward-kdl-in-ward.md](ward-kdl-in-ward.md) - native-path isolation.
