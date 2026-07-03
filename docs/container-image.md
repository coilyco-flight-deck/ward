---
doc_goal: Tell a reader exactly which dev-base image every warded run pulls, that ward runs it unmodified with no ward or repo baked in, how anonymous pull works, and how a security-conscious adopter pins off the moving latest tag - so the image is never an opaque download.
---
# the container dev-base image

Every [`ward agent`](agent.md) run pulls **one** image: the aos-published
**dev-base**. This is what actually downloads and runs the first time you type
`warded #N` on a fresh host. ward runs it **unmodified**, bind-mounting only its
own entrypoint + doctrine and downloading ward itself, so the image bakes in no
ward and no target repo (cloned fresh at run).

## The ref, registry, and pull access

- **Ref** - **`forgejo.coilysiren.me/coilyco-flight-deck/agentic-os:latest`**. The
  ref and its `:latest` tag are the compiled-in defaults (`containerImageDefault`
  / `containerImageTagDefault` in
  [`cmd/ward/container_compute.go`](../cmd/ward/container_compute.go)).
- **Registry** - the **Forgejo container registry** at `forgejo.coilysiren.me`,
  the same host that carries the canonical repos.
- **Pull access** - **anonymous pull works**. A cold user or a fresh host can
  `docker pull` it with no login, token, or account, so the first run just
  downloads it:

```bash
docker pull forgejo.coilysiren.me/coilyco-flight-deck/agentic-os:latest
```

## What ships in it

uv, pre-commit, node, go, the aws + tailscale CLIs, the pinned agent harnesses
(claude/codex/goose), the lint / secret-scan / format binaries, and a baked
public substrate seed. The canonical inventory + build pipeline is aos's
[`docs/dev-base-image.md`](https://forgejo.coilysiren.me/coilyco-flight-deck/agentic-os/src/branch/main/docs/dev-base-image.md).

## Tag policy and pinning off the moving tag

`:latest` is a **moving tag**. Each aos release also tags `vX.Y.Z` and re-points
`:latest` at it, so a fresh run tracks the newest dev-base rather than a frozen
one. A detached run names its pull up front and beats a `still pulling` heartbeat
([ward#322](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/322)), so a first-launch download is not invisible even when nobody watches.

A security-conscious adopter who does not want `latest` shifting under them -
the agent runs `bypassPermissions` ([container-permissions.md](container-permissions.md))
inside this image - pins it:

- **Per run** - `--image` / `--tag` ([agent-flags.md](agent-flags.md)); `--tag
  vX.Y.Z` freezes a run to one released image.
- **Once for every dispatch** - `WARD_AGENT_IMAGE` / `WARD_AGENT_TAG` ([ward#312](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/312)).
- **`--no-pull`** reuses the already-cached image without re-pulling.

## See also

[container.md](container.md) - the container model and lifecycle.
[container-substrate.md](container-substrate.md) - reference repos seeded from this image.
[FEATURES.md](FEATURES.md) - inventory.
