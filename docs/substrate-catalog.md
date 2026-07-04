---
doc_goal: Pin what the auto-generated substrate repo catalog is - a Forgejo-derived index (full_name + description + topics + mount path) baked into the seed as the read-these-first backing data - who owns the generator vs the trigger, and how compose degrades with no catalog.
---
# Substrate repo catalog ([ward#594](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/594))

The substrate warms reference repos under `/substrate/<name>`
([container-substrate.md](container-substrate.md)) and labels them as a
read-these-first block ([ward#593](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/593)).
That block self-sourced a tagline from each repo's `README.md` - good prose, blind
to two facts a session keeps needing:

- the **canonical `full_name`** (owner/name from Forgejo). A README says nothing
  about who owns a repo, so an agent guesses one off ambient naming
  (`coilysiren/ward`) and only learns the canonical `coilyco-flight-deck/ward` when
  a mutating write trips Forgejo's rename-redirect.
- the repo's **topics**, which live in Forgejo, not the working tree.

The catalog carries both. It is **auto-generated**, never hand-maintained: a
hand-written list is the point-in-time artifact the doctrine warns rots, so it
derives from Forgejo on a trigger and Forgejo stays the single source of truth.

## The artifact

`substrate-catalog.json`, one entry per repo on the substrate manifest
([`preclone-repos.txt`](../cmd/ward/containerassets/preclone-repos.txt)):

```json
{ "schema": 1, "repos": [ {
  "full_name": "coilyco-flight-deck/infrastructure",
  "description": "k3s cluster, systemd units, and invoke tasks",
  "topics": ["k3s", "homelab"], "tier": "image",
  "mount_path": "/substrate/infrastructure" } ] }
```

`full_name`, `description`, and `topics` are Forgejo's own fields; `tier` is the
manifest's public/private seed tier; `mount_path` is the reference surface. Entries
sort by `full_name` for a byte-stable file; `schema` guards the shape.

## Who owns what (config-placement law)

- **ward owns the generator.** `ward container substrate-catalog` (hidden) reads
  the embedded manifest, lists each unique owner once through `ward ops forgejo`,
  and emits the catalog. `--out` writes a file (default stdout), `--dest` sets the
  `/substrate` root recorded as each `mount_path`, `--tier` restricts to one seed
  tier. A repo Forgejo omits still gets an entry off its own owner/name, so a
  transient miss never drops a warmed repo.
- **infra only triggers the regen.** The seed-refresh trigger (a push or the seed
  cron) runs the generator with `--out` pointed at the substrate seed:

  ```
  ward container substrate-catalog --tier image --out "$WARD_SUBSTRATE_SEED/substrate-catalog.json"
  ```

  Same authoring-vs-rollout split as every other rollout: ward authors, infra rolls.
  `--tier image` is required, not cosmetic: the public seed must not bake a private
  cache-tier (`coilyco-bridge`) repo's `full_name` or topics, so it drops them first.

## Consumption

At compose the hidden `ward container substrate-inventory` reads the baked catalog
from `WARD_SUBSTRATE_SEED` and enriches each mounted-repo bullet with the canonical
`full_name`, `description`, and `topics`. The lookup is **best-effort**: with no
catalog (before infra wires the trigger, or a parse miss) each bullet falls back to
the repo's README tagline, so the enrichment never breaks a run. The block is
one-sourced across the bash and Go compose paths through that one command.

[ward#593](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/593) labels
the *relevant subset* for a session; this catalog is the *full addressable index*
selection reads from, and independently kills the owner-guessing bug via canonical
`full_name`s.

## See also

- [container-substrate.md](container-substrate.md) - the `/substrate` layer the catalog indexes.
- [ward-kdl-surface.md](ward-kdl-surface.md) - the `ward ops forgejo` surface the generator queries.
