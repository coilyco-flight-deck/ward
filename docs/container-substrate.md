---
doc_goal: Explain the /substrate reference-repo layer as what lets a least-access boxed agent read a convention or contract without reaching outside its container - the public/private tier split that keeps the dev-base image shareable, TTL-gated mirror warming, and the read-from-either / act-only-on-workspace rule when a repo lands in both trees.
---
# container substrate reference repos

Beyond the target repo, every `ward container` warms a fixed set of cross-cutting
**reference repos** - doctrine, skills, the cross-repo contracts, the dev/ops
CLIs - so an agent can read a convention without reaching outside its box. The
canonical list is [`preclone-repos.txt`](../cmd/ward/containerassets/preclone-repos.txt),
`owner/name  tier` per line, embedded in the binary and parsed by both Go and
the entrypoint. They land under `/substrate/<name>`.

## Tiers

Each entry carries a tier, split on a public/private boundary so the published
dev-base image stays shareable:

- `image` - public (coilysiren + coilyco-flight-deck). A bare-mirror seed is also
  baked into the aos dev-base image at `/opt/substrate-seed`, so a cold host
  warms these with no network. Built by aos, see its [`docs/dev-base-image.md`](https://forgejo.coilysiren.me/coilyco-flight-deck/agentic-os/src/branch/main/docs/dev-base-image.md).
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

Warming is **best-effort** - any failure logs and the container continues.
`WARD_SUBSTRATE_SKIP=1` skips it entirely. The agent-facing note lives in
[AGENTS.container.md](../cmd/ward/containerassets/AGENTS.container.md).

## Labeling the mounts ([ward#593](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/593))

Warming the repos is not enough. A mount the agent is never told to read is a
silent pile on disk, and a session that does not spelunk it falls back to
interrogating the operator for facts already checked out (a public IP, a Caddy
route, a `*.coilysiren.me` subdomain - all in `/substrate/infrastructure`): the
"'discoverable in the clone' is a trap" failure the doctrine names.

So the composed context ends with a **read-these-first** block: one bullet per
warmed `/substrate/<name>`, each with a self-sourced tagline from that repo's own
`README.md` (then `AGENTS.md`, then `docs/FEATURES.md`) so the label never drifts.
The block is one-sourced across the bash and Go compose paths via the hidden `ward
container substrate-inventory` command. When the seed carries a
[substrate catalog](substrate-catalog.md) the bullets are enriched with each repo's
canonical `full_name`, description, and Forgejo topics ([ward#594](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/594)).

## When a repo lands in both trees

Because the substrate copy and the target/granted clones hydrate from the same
`ward-gitcache` mirror, a manifest repo that is *also* the target (or a `--repo`
grant) ends up under **both** `/substrate/<name>` and `/workspace/<name>` at the
same HEAD. That overlap is expected: the split is by *role*, not by which repos
exist where. `/workspace/<name>` is authoritative for work; `/substrate/<name>`
stays read-only reference even for a repo being actively changed. The doctrine
spells out the read-from-either / act-only-on-`/workspace` rule in
[AGENTS.container.md](../cmd/ward/containerassets/AGENTS.container.md).

## See also

[docs/container.md](container.md) - the container model and lifecycle.
