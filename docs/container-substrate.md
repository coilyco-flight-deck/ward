# container substrate reference repos

Beyond the target repo, every `ward container` warms a fixed set of cross-cutting
**reference repos** - doctrine, skills, the cross-repo contracts, the dev/ops
CLIs - so an agent can read a convention without reaching outside its box. The
canonical list is [`preclone-repos.txt`](../cmd/ward/containerassets/preclone-repos.txt),
`owner/name  tier` per line, embedded in the binary and parsed by both Go (for
validation) and the entrypoint (to warm). They land under `/substrate/<name>`.

## Tiers

Each entry carries a tier, split on a public/private boundary so the published
dev-base image stays shareable:

- `image` - public (coilysiren + coilyco-flight-deck). A bare-mirror seed is also
  baked into the aos dev-base image at `/opt/substrate-seed`, so a cold host
  warms these with no network. Built by aos, see its `docs/dev-base-image.md`.
- `cache` - coilyco-bridge (leak-tolerant/private). Never baked into the image.
  Cloned over the network on first use.

## Warming

Both tiers live in the shared `ward-gitcache` volume as bare mirrors, with a
local working copy under `/substrate/<name>`. The mirror is refreshed by a
**TTL-gated fetch** (`WARD_SUBSTRATE_TTL`, default 600s): the first container past
the TTL does one fetch per repo, the rest skip the gate, and an `flock`
serialises concurrent inits against a given mirror. On a cold volume an
image-tier repo hydrates from its baked seed (a local copy, no network) instead
of cloning.

Warming is **best-effort** - any failure logs and the container continues, since
the target work is the job. `WARD_SUBSTRATE_SKIP=1` skips it entirely. The
agent-facing note lives in [AGENTS.container.md](../cmd/ward/containerassets/AGENTS.container.md).

## See also

[docs/container.md](container.md) - the container model and lifecycle.
