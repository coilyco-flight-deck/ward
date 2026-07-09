---
doc_goal: Explain ward's read-only bridge surface as a consumer of infrastructure-owned coordination facts, so a reader sees ward as the command surface and infrastructure as the state owner rather than a second source of truth.
---
# `ward ops bridge`

`ward ops bridge` is the read-only command surface for the GitHub <=> Forgejo
coordination hub. Ward does **not** own the bridge facts. Infrastructure does.
Ward only reads the machine-published bundle and renders it through a small set
of operator verbs.

The bundle is launch-selected through the same `WARD_CONFIG_REF` fs.FS seam the
rest of ward's edge-mounted config uses. The baked default is a neutral empty
bundle, so a missing infrastructure publication fails loud instead of silently
inventing repo mappings.

## Surface

- `ward ops bridge authoritative-side <owner/repo>` - print the authoritative
  side for one repo.
- `ward ops bridge mirror-status [owner/repo]` - print one repo's mirror status,
  or the full set when no repo is named.
- `ward ops bridge divergent-refs [owner/repo]` - print the divergent refs for
  one repo.
- `ward ops bridge stale-syncs` - print the repos whose sync status is stale or
  otherwise non-green.
- `ward ops bridge map-issue <ref>` - map an issue ref back to the repo's
  authority and tracker side.

## Shape

The current bridge bundle shape is intentionally small:

```json
{
  "schema": 1,
  "repos": [{
    "full_name": "owner/repo",
    "authoritative_side": "forgejo",
    "mirror_targets": ["github"],
    "tracker_authority": "forgejo",
    "mirror_status": "in_sync",
    "last_sync_age": "5m"
  }]
}
```

That is a consumer contract, not a second registry. Infrastructure owns the
publication of the bundle and its contents. Ward owns the command surface and
the presentation.

## See also

- [FEATURES.md](FEATURES.md) - what ships today.
- [README.md](../README.md) - the high-level product overview.
