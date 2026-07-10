# ward setup onboarding skeleton

`ward setup` is the live cache warmer / config doctor for ward's selected
runtime source. It pre-bakes and validates the config surfaces without building
or replacing the ward binary, and it is not a hidden prerequisite for normal
ward commands.

This page keeps the old setup/doctor shape around as context for the rebirth
pass, but the live meaning is the new setup flow below.

## First slice

When `WARD_CONFIG_REF` is set:

- resolve the configured source ref using the same config-source resolver used at runtime
- fetch or refresh the cached checkout
- report the resolved commit SHA and cache path
- parse and compile the configured surfaces once: ops, exec, fleet, and smart defaults
- fail loud with actionable diagnostics if any surface is broken
- avoid printing secrets
- print a concise success summary naming the active source, resolved SHA, cache location, and validated surfaces

When `WARD_CONFIG_REF` is unset:

- validate the baked neutral/default source
- explain that no external config source is active

## Onboarding skeleton

The command is structured in phases so it can grow without changing meaning:

1. config source
2. auth / credential checks (stub or minimal for now)
3. cache warm
4. surface compile
5. host integration checks (stub or minimal for now)

Only the config, cache, and compile phases are complete in this slice.

## Notes

- `ward doctor` remains retired.
- Normal `ward` commands still work and fail correctly if setup has never been run.
- The command should surface the resolved source, SHA, cache path, and validated surfaces without claiming future phases are done.
