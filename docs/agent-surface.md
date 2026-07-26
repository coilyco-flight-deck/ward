---
doc_goal: Keep the read-only agent surface anchor stable after the old page was collapsed.
---
# agent surface

This page is the durable anchor for the read-only agent surface.

- It covers the seedless interactive surface that drops into drain.
- It names the `WARD_READONLY` restriction and brokered read-only access.
- It separates the supervisory lane from the detached engineer path.
- It starts an independently supervised broker service before the attached
  director. The director reaches it at `broker:7420` on the Compose project
  network. Both containers appear under the same Compose application. Director
  or terminal exit removes only the director and leaves that broker running.
- It keeps the director surface's cleanup path visible. Fresh director surfaces
  mount the Docker socket for local `ward agent reap`. Already-running surfaces
  that predate that mount need a restart to pick it up. Until then, use the
  brokered `ward agent stop <owner/repo#N>` cleanup path from the surface.
- It stores the stack assets, Compose definition, and private launch environment
  under `~/.ward/director-stacks/<project>/`. Broker request journals,
  reservation sentinels, and dispatch artifacts stay in the existing `~/.ward`
  trees.
- It gives the read-only director surface a gitcache-backed scratch and Go cache root so focused verification has writable space.
- It links the doctrine-promised `/scratch` path to that gitcache-backed root, so the escape hatch the composed doctrine names exists on read-only surfaces too ([ward#1142](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1142)).
- It prints the scratch root and budget at startup, then fails loudly with a warning-marked Docker resource constraint if that writable space is too small for focused Go verification.

## See also

- [agent-director.md](agent-director.md) - the read-only director lane.
- [container-contract.md](container-contract.md) - mounts, env, and permissions.
