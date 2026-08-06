# ward doctor

`ward doctor` validates Ward's typed defaults and supported YAML before a run
leans on them.

## What it checks

- typed defaults and repository authority.
- route-mode intake repository settings.
- typed harness adapters and fixed workflows.
- compiled launch payloads.
- operator-local redaction environment names and RE2 patterns.
- placeholder or example values that should not survive in an operating deployment.
- `WARD_DOCTOR_ALLOW_PLACEHOLDERS=1` permits the baked ward surface to carry its
  sentinel values without failing the placeholder checks.

## Output

- grouped `PASS` / `FAIL` lines.
- non-zero exit on any failure.
- a warning with the exact `~/.ward/agent-logs/` location when historical raw
  archives remain. Ward does not migrate, sanitize, or delete them.

## See also

- [config-source.md](config-source.md) - runtime setting ownership.
- [enforcement-boundary.md](enforcement-boundary.md) - the executable boundary.
