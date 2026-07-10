# ward doctor

`ward doctor` validates the resolved runtime config before a run leans on it.

## What it checks

- launch-time config source resolution.
- smart-defaults and repo-authority policy.
- fleet defaults and required roles.
- guarded ops and exec bundle inputs.
- placeholder or example values that should not survive in an operating deployment.

## Output

- grouped `PASS` / `FAIL` lines.
- non-zero exit on any failure.

## See also

- [config-source.md](config-source.md) - launch-time config resolution.
- [ward-kdl.md](ward-kdl.md) - the bundle authoring layer.
