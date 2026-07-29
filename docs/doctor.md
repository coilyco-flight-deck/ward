# ward doctor

`ward doctor` validates Ward's typed defaults and supported YAML before a run
leans on them.

## What it checks

- typed defaults and repository authority.
- route-mode intake repository settings.
- typed harness adapters and fixed workflows.
- compiled launch payloads.
- placeholder or example values that should not survive in an operating deployment.
- `WARD_DOCTOR_ALLOW_PLACEHOLDERS=1` permits the baked ward surface to carry its
  sentinel values without failing the placeholder checks.

## Output

- grouped `PASS` / `FAIL` lines.
- non-zero exit on any failure.

## See also

- [config-source.md](config-source.md) - runtime setting ownership.
- [aosguard-boundary.md](aosguard-boundary.md) - the AOSguard boundary.
