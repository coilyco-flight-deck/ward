# ward-kdl ops eco native

Exec-dialect CLI. Every verb runs `pwsh` with the granted subcommand (or its `argv` override) appended; the binary and its prefix are fixed and the caller can never substitute them.

## ward-kdl ops eco native test - smoke-gate the working copy: boot a throwaway world, fail-closed on ready/ModKit/compile/mods markers. Args: [src] [mods-csv]

`pwsh -NoProfile -File X:/projects/coilyco-flight-deck/infrastructure/scripts/eco-native-smoke.ps1`

Flags: unrestricted passthrough.

## ward-kdl ops eco native snapshot - copy live Mods/+Configs/ aside; prints the snapshot id as the final line

`pwsh -NoProfile -File X:/projects/coilyco-flight-deck/infrastructure/scripts/eco-native-snapshot.ps1`

Flags: unrestricted passthrough.

## ward-kdl ops eco native apply - overlay a working-copy Mods/+Configs/ onto the install (the tested artifact, ward#584)

`pwsh -NoProfile -File X:/projects/coilyco-flight-deck/infrastructure/scripts/eco-native-apply.ps1`

Flags: unrestricted passthrough.

## ward-kdl ops eco native restart - stop the install's EcoServer process and relaunch headless, run logs truncated

`pwsh -NoProfile -File X:/projects/coilyco-flight-deck/infrastructure/scripts/eco-native-restart.ps1`

Flags: unrestricted passthrough.

## ward-kdl ops eco native await - bounded wait for a healthy post-restart boot (Eco boots take minutes)

`pwsh -NoProfile -File X:/projects/coilyco-flight-deck/infrastructure/scripts/eco-native-await.ps1`

Flags: unrestricted passthrough.

## ward-kdl ops eco native health - one instant probe: service_active / journal_clean / server_ready key=val

`pwsh -NoProfile -File X:/projects/coilyco-flight-deck/infrastructure/scripts/eco-native-health.ps1`

Flags: unrestricted passthrough.

## ward-kdl ops eco native rollback - restore a snapshot, restart, and VERIFY recovery (non-zero when still unhealthy)

`pwsh -NoProfile -File X:/projects/coilyco-flight-deck/infrastructure/scripts/eco-native-rollback.ps1`

Flags: unrestricted passthrough.

## See also

- [ward-kdl.md](../ward-kdl.md) - the build-time authoring layer behind this surface
- [ward-kdl-surface.md](../ward-kdl-surface.md) - the full generated verb surface, area by area
