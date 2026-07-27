# ward doctor

`ward doctor` validates Ward's embedded native policy before a run leans on it.

## What it checks

- smart-defaults and repo-authority policy.
- baked fleet defaults and required roles.
- native launch assets.
- placeholder or example values that should not survive in an operating deployment.
- `WARD_DOCTOR_ALLOW_PLACEHOLDERS=1` permits the baked ward surface to carry its
  sentinel values without failing the placeholder checks.

## Output

- grouped `PASS` / `FAIL` lines.
- non-zero exit on any failure.

## See also

- [config-source.md](config-source.md) - native-policy boundary.
- [aosguard-boundary.md](aosguard-boundary.md) - the AOSguard boundary.
