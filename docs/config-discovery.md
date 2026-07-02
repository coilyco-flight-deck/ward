# Config discovery

ward resolves the allowlist path in this order:

1. `--config <path>` on the root command (e.g. `ward --config /abs/ward.yaml exec build`).
2. `$WARD_CONFIG` in the environment.
3. Walk up from cwd and use the first reachable allowlist.

`--config` wins over `$WARD_CONFIG`, and either override skips the walk-up
search. The eventual `repocfg.Load` call is the existence check and produces
the user-facing error if the chosen path is missing.

## Candidate filenames

- `.ward/ward.yaml` - canonical home.
- `.coily/coily.yaml` - honored so repos already using coily's
  allowlist do not have to rename the file to adopt ward.

Both use the cli-guard `repocfg` format.

## Notes

- `repocfg.Load` parses the chosen file and applies cli-guard's argv policy
  checks while loading the allowlist.
- If nothing is reachable during walk-up, ward reports that no config could be
  found.
