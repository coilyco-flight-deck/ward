# ward-kdl ops eco observe

Exec-dialect CLI. Every verb runs `ssh -o BatchMode=yes kai@kai-server` with the granted subcommand (or its `argv` override) appended; the binary and its prefix are fixed and the caller can never substitute them.

## ward-kdl ops eco observe status - live systemd unit state for eco-server.service (read-only; bare name matches the unit, --no-pager for a full capture)

`ssh -o BatchMode=yes kai@kai-server systemctl status eco-server.service --no-pager`

Sealed: the pinned command forwards exactly; no trailing caller arguments are accepted.

Flags: unrestricted passthrough.

## ward-kdl ops eco observe logs - tail the last 500 journal lines for eco-server.service (read-only; watch a restart, eco-ops#24/#26)

`ssh -o BatchMode=yes kai@kai-server journalctl -u eco-server.service -n 500 --no-pager`

Sealed: the pinned command forwards exactly; no trailing caller arguments are accepted.

Flags: unrestricted passthrough.

## ward-kdl ops eco observe mods - list the live Mods/ tree - audit the installed mod set + DLL versions (eco-ops#15)

`ssh -o BatchMode=yes kai@kai-server ls -la /home/kai/Steam/steamapps/common/EcoServer/Mods`

Sealed: the pinned command forwards exactly; no trailing caller arguments are accepted.

Flags: unrestricted passthrough.

## ward-kdl ops eco observe configs - list the live Configs/ tree - confirm a config file landed (eco-ops#21)

`ssh -o BatchMode=yes kai@kai-server ls -la /home/kai/Steam/steamapps/common/EcoServer/Configs`

Sealed: the pinned command forwards exactly; no trailing caller arguments are accepted.

Flags: unrestricted passthrough.

## ward-kdl ops eco observe read-config - cat one live Eco config file, e.g. .../Configs/EcoTelemetry.json (eco-ops#26) or a Configs/*.eco (eco-ops#21). Pass the absolute path; traversal + obvious-secret paths are denied, and cat is read-only regardless.

`ssh -o BatchMode=yes kai@kai-server cat`

Flags: unrestricted passthrough.

Preflight:

- denies when arg0 matches *..* or */.ssh/* or *id_rsa* or *id_ed25519* or */.aws/* or *credential* or *secret*

## See also

- [ward-kdl.md](../ward-kdl.md) - the build-time authoring layer behind this surface
- [ward-kdl-surface.md](../ward-kdl-surface.md) - the full generated verb surface, area by area
