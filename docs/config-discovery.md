# Config discovery

ward resolves the allowlist path in this order:

1. `--config <path>` on the root command (e.g. `ward --config /abs/ward.yaml exec build`).
2. `$WARD_CONFIG` in the environment.
3. Walk-up from cwd looking for the first reachable allowlist (legacy behavior).

`--config` and `$WARD_CONFIG` are returned verbatim (made absolute) without
a stat. The eventual `repocfg.Load` call is the canonical existence check
and produces a clearer error than a duplicate stat here. Walk-up is only
attempted when neither override is set.

The `--config` flag carries `WARD_CONFIG` as its env source so urfave-level
help shows the association. Resolution for `exec` (whose subtree is built
at init time, before urfave parses flags) is driven by `preParseConfigFlag`,
which scans `os.Args` for `--config` / `--config=` and stops at `--` or the
first positional.

## Candidate filenames

- `.ward/ward.yaml` - canonical home.
- `.coily/coily.yaml` - honored so repos already using coily's
  allowlist do not have to rename the file to adopt ward.

Both use the cli-guard `repocfg` format.

## loadDefault

Resolves the path via `resolveConfigPath(explicit, env, cwd)` then parses
it through cli-guard's `repocfg` loader, which runs every argv token
through the shell-metacharacter policy check.

## discoverConfig

Walks up from `start` looking for the first reachable allowlist.
Returns the absolute path on success or `errNoConfig` if nothing is
reachable.
