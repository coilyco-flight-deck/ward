#!/usr/bin/env bash
# ward container entrypoint. Bind-mounted into the aos dev-base image at
# /opt/agentic-os/ward-shell-entrypoint.sh by `ward agent` at container
# bring-up (it is embedded in the ward binary, not baked into the image). The
# shell now stays as a thin POSIX shim: it links to the ward binary the host
# already staged into the assets dir, verifies it, then hands startup off to
# `ward container bootstrap`.
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
  exec /usr/local/bin/ward container bootstrap "$@"
}

main "$@"
