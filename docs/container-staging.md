---
doc_goal: Define host launch staging placement, override precedence, token-file security, mount validation, and legacy cleanup.
---
# container host staging

Ward materializes each run's assets directory and credential-bearing Docker
env-file together under one host staging root.

## Defaults and overrides

The defaults are:

- Windows: `%LOCALAPPDATA%\ward\staging`.
- macOS and Linux: `~/.ward/staging`.
- If platform state is unavailable: the system temporary directory under
  `ward/staging`.

An operator can set a per-host root in `~/.ward/config.yaml`:

```yaml
container:
  staging-dir: /absolute/host/path
```

Precedence is `container.staging-dir`, the process-only `WARD_STAGING_DIR`,
then the platform default. The older `WARD_LAUNCH_STAGING_DIR` spelling remains
a compatibility alias after the operator setting. Host placement never belongs
in repository YAML or an embedded fleet-tuning surface.

## Credential boundary

Ward secures the env-file before writing credentials. Unix applies mode `0600`.
Windows replaces inherited permissions with a protected, current-user-only
DACL and reads it back to verify the result. The launch fails if the filesystem
cannot enforce that ACL.

On Windows, bring-up also mounts the exact staged assets directory into a
credential-free probe container and checks that `entrypoint.sh` is readable.
An inaccessible custom drive fails before credential handoff with a diagnostic
naming Docker Desktop's **Settings > Resources > File sharing** control.

## Cleanup and Linux

Both stale sweeps follow the resolved root. A migration sweep also checks the
old home/profile root for past-TTL `ward-forgejo-env-*` files and
`ward-container-assets-*` directories.

Snap Docker cannot access a top-level hidden home directory. Ward already
refuses snap Docker because other required host binds are inaccessible. Native
Docker is the supported Linux path, so `~/.ward/staging` does not weaken a
supported snap launch.

## See also

- [container-contract.md](container-contract.md) - mounts, env, permissions.
- [container-lifecycle.md](container-lifecycle.md) - launch and teardown.
