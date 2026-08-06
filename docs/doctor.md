---
doc_goal: State Ward doctor's prerequisites, exact inputs, checks, failures, and operator remedies.
---
# `ward doctor`

`ward doctor` validates the typed product defaults plus supported operator and
repository YAML. It does not start Docker, contact a forge, mutate config, or
repair a failed setting.

## Inputs

* Ward's compiled smart defaults and typed harness registry.
* `~/.ward/config.yaml`, when present.
* The discovered `.ward/ward.yaml`, when the current directory has one.
* Operator-local redaction rules used while loading config.

## Checks

* `smart defaults` validates route intake, workflow values, positive limits,
  redaction environment names, and RE2 redaction patterns.
* `repo authority` requires trusted owners and valid repository routing rules,
  and requires the route-intake owner to be trusted.
* `harness adapters` validates every built-in harness definition and launch
  payload.
* Operational placeholder values fail unless
  `WARD_DOCTOR_ALLOW_PLACEHOLDERS=1` explicitly admits fixture configuration.

The report prints its source summary, then one `PASS` or `FAIL` row per check.
Any failed row makes the command exit nonzero. A separate warning names
`~/.ward/agent-logs/` when retired raw archives still exist. Ward does not
read, migrate, sanitize, or delete those archives.

## Remedies

* Fix malformed YAML or unsupported values in the input path named by the error.
* Replace placeholder owners or repositories with operating values.
* Make the route-intake owner part of the trusted owner set.
* Use a supported workflow or harness name from the command help.
* Fix invalid redaction names or patterns in `~/.ward/config.yaml`.
* Use the placeholder override only for a deliberate fixture or source-tree validation.

## See also

* [config-source.md](config-source.md) - exact config ownership and precedence.
* [ward-yaml.md](ward-yaml.md) - repository YAML schema.
