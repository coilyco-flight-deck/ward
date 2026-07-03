---
doc_goal: Let a contributor know exactly how ward finds its allowlist config - the override precedence, the two honored filenames, and the loud-failure rules - so config resolution is never a mystery and a coily-era repo adopts ward without renaming a file.
---
# Config discovery

ward resolves the allowlist path in this order:

1. `--config <path>` on the root command (e.g. `ward --config /abs/ward.yaml exec build`).
2. `$WARD_CONFIG` in the environment.
3. Walk up from cwd and use the first reachable allowlist.

`--config` wins over `$WARD_CONFIG`, and either override skips the walk-up
search entirely.

## Candidate filenames

- `.ward/ward.yaml` - canonical home.
- `.coily/coily.yaml` - honored so repos already using coily's
  allowlist do not have to rename the file to adopt ward.

Both use the cli-guard `repocfg` format.

## Errors

- A path set by `--config` or `$WARD_CONFIG` that does not exist fails loudly
  and names the missing file - an explicit override is never silently ignored
  or downgraded to the walk-up.
- When the walk-up reaches the filesystem root without finding either candidate
  filename, ward reports that no config could be found.
