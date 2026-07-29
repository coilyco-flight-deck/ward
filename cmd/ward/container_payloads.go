package main

// Container launch payloads are compiled into Ward and materialized by
// writeContainerAssetsAt; no source-side asset tree or embed.FS is involved.
const containerEntrypointScript = `#!/usr/bin/env bash
# ward container entrypoint. Bind-mounted into the dev-base image at
# /opt/ward-shell-entrypoint.sh by ward agent at container bring-up. The shell
# stays as a thin POSIX shim: it links to the ward binary the host already
# staged, verifies it, then hands startup off to ward container bootstrap.
set -euo pipefail

log() { printf 'ward-container: %s\n' "$*" >&2; }

: "${WARD_TARGET_OWNER:?missing WARD_TARGET_OWNER}"
: "${WARD_TARGET_NAME:?missing WARD_TARGET_NAME}"
: "${WARD_FORGEJO_BASE:?missing WARD_FORGEJO_BASE}"

install_ward() {
  install -m 0755 /opt/ward/ward /usr/local/bin/ward
  ln -sf ward /usr/local/bin/warded
  /usr/local/bin/ward version >&2 || die "ward did not install correctly"
  /usr/local/bin/warded --help >/dev/null 2>&1 || die "warded did not install correctly"
}

die() { log "fatal: $*"; exit 1; }

main() {
  install_ward
  if [[ "${WARD_CONTAINER_SERVICE:-}" == "dispatch-broker" ]]; then
    exec /usr/local/bin/ward container dispatch-broker
  fi
  exec /usr/local/bin/ward container bootstrap "$@"
}

main "$@"
`

const containerDoctrine = `Container agent doctrine

You are inside an ephemeral ward feature container. This file overrides host defaults for this run.

What this container is:
- A throwaway box for one feature from start to merge.
- The writable workspace is /workspace/<name>.
- /substrate/<name> is read-only reference.
- If a repo appears in both trees, work only in /workspace.

What to do:
1. Implement the feature on the feature branch.
2. Commit the work.
3. Merge into main or the target branch as required by the forge.
4. Push the result.

GitHub-hosted runs:
- If the target forge is GitHub, push the feature branch and open a PR.
- Do not push GitHub main directly.

Limits:
- No force-pushes.
- No deleting shared refs.
- No touching repos outside the target and any explicitly granted extras.
- No destructive actions outside the normal feature loop.

Additional granted repos:
- Work in any explicitly granted extra repo the same way you work in the target repo.
- Push each one yourself before exit.

Reaper backstop:
- The reaper cleans up loose work after exit.
- It is a backstop, not a substitute for finishing.
- Leave no uncommitted feature work behind.

Reference repos:
- /substrate holds read-only reference copies of shared repos.
- Read conventions there when you need them, but do not edit them.

End state:
- Finish the feature end to end.
- Leave the tree clean and the landing path complete.
`

const containerSettingsJSON = `{
  "tui": "fullscreen",
  "deniedMcpServers": [
    {
      "serverName": "claude-in-chrome"
    }
  ],
  "permissions": {
    "defaultMode": "bypassPermissions"
  }
}
`

const defaultSubstrateManifest = `# Ward's bundled substrate manifest contains only the public example repo.
# Deployments own their repo roster instead of compiling one user's
# repositories into Ward.
#
# Deployment-provided rows use this whitespace-delimited format:
#   owner/name  image|cache

coilysiren/example  image
`
