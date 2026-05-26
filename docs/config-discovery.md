# Config discovery

agent-guard walks up from cwd looking for the first reachable allowlist.

## Candidate filenames

- `.agent-guard/agent-guard.yaml` - canonical home.
- `.coily/coily.yaml` - honored so repos already using coily's
  allowlist do not have to rename the file to adopt agent-guard.

Both use the cli-guard `repocfg` format.

## loadDefault

Discovers the config from cwd and parses it via cli-guard's `repocfg`
loader, which runs every argv token through the shell-metacharacter
policy check.

## discoverConfig

Walks up from `start` looking for the first reachable allowlist.
Returns the absolute path on success or `errNoConfig` if nothing is
reachable.
