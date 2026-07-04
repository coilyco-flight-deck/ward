# ward-kdl ops eco server

Exec-dialect CLI. Every verb runs `ssh -o BatchMode=yes kai@kai-server` with the granted subcommand (or its `argv` override) appended; the binary and its prefix are fixed and the caller can never substitute them.

## ward-kdl ops eco server snapshot - copy live Mods/+Configs/ aside on kai-server; prints the snapshot id as the final line

`ssh -o BatchMode=yes kai@kai-server bash /home/kai/projects/coilyco-flight-deck/infrastructure/scripts/eco-server-snapshot.sh`

Flags: unrestricted passthrough.

## ward-kdl ops eco server stage-clean - drop any stale staged upload so scp lands a fresh tree

`ssh -o BatchMode=yes kai@kai-server rm -rf /tmp/ward-eco-apply`

Sealed: the pinned command forwards exactly; no trailing caller arguments are accepted.

Flags: unrestricted passthrough.

## ward-kdl ops eco server stage - upload the tested working-copy tree to the kai-server staging dir (ward#584)

`ssh -o BatchMode=yes kai@kai-server -o BatchMode=yes -r`

Flags: unrestricted passthrough.

## ward-kdl ops eco server apply - merge the staged working-copy Mods/+Configs/ into the live tree

`ssh -o BatchMode=yes kai@kai-server bash /home/kai/projects/coilyco-flight-deck/infrastructure/scripts/eco-server-apply-staged.sh`

Flags: unrestricted passthrough.

## ward-kdl ops eco server restart - restart the live unit (bare name: matches the NOPASSWD sudoers grant)

`ssh -o BatchMode=yes kai@kai-server sudo systemctl restart eco-server`

Sealed: the pinned command forwards exactly; no trailing caller arguments are accepted.

Flags: unrestricted passthrough.

## ward-kdl ops eco server await - bounded wait for a healthy post-restart boot (Eco boots take minutes)

`ssh -o BatchMode=yes kai@kai-server bash /home/kai/projects/coilyco-flight-deck/infrastructure/scripts/eco-server-await-healthy.sh`

Flags: unrestricted passthrough.

## ward-kdl ops eco server health - one instant probe: service_active / journal_clean / server_ready key=val

`ssh -o BatchMode=yes kai@kai-server bash /home/kai/projects/coilyco-flight-deck/infrastructure/scripts/eco-server-health-check.sh`

Flags: unrestricted passthrough.

## ward-kdl ops eco server rollback - restore a snapshot, restart, and VERIFY recovery (non-zero when still unhealthy)

`ssh -o BatchMode=yes kai@kai-server bash /home/kai/projects/coilyco-flight-deck/infrastructure/scripts/eco-server-rollback.sh`

Flags: unrestricted passthrough.

## See also

- [ward-kdl.md](../ward-kdl.md) - the build-time authoring layer behind this surface
- [ward-kdl-surface.md](../ward-kdl-surface.md) - the full generated verb surface, area by area
