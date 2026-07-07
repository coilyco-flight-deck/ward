#!/usr/bin/env bash
# ward container entrypoint. Bind-mounted into the aos dev-base image at
# /opt/ward/entrypoint.sh by `ward agent` at container bring-up (it is embedded
# in the ward binary, not baked into the image). The shell stays generic and
# hands startup off to `ward container bootstrap`, which owns the per-agent
# setup through the agentsapi surface.
set -euo pipefail

log() { printf 'ward-container: %s\n' "$*" >&2; }

: "${WARD_TARGET_OWNER:?missing WARD_TARGET_OWNER}"
: "${WARD_TARGET_NAME:?missing WARD_TARGET_NAME}"
: "${WARD_FORGEJO_BASE:?missing WARD_FORGEJO_BASE}"

install_ward_from_source() {
  log "building ward from mounted source $WARD_FROM_SOURCE"
  ( cd "$WARD_FROM_SOURCE" \
    && GOPROXY=direct GOSUMDB=off go build -o /usr/local/bin/ward ./cmd/ward )
}

resolve_ward_tag() {
  local tag="${WARD_VERSION:-}"
  if [ -z "$tag" ] || [ "$tag" = "dev" ]; then
    tag="$(curl -fsSL -H "Authorization: token ${FORGEJO_TOKEN:-}" \
      "$WARD_FORGEJO_BASE/api/v1/repos/coilyco-flight-deck/ward/releases/latest" \
      | jq -r '.tag_name')" || tag=""
  fi
  printf '%s' "$tag"
}

install_ward_from_release() {
  local tag asset
  tag="$(resolve_ward_tag)"
  [ -n "$tag" ] && [ "$tag" != "null" ] || die "could not resolve a ward release tag (set --ward-source to build instead)"
  asset="$WARD_FORGEJO_BASE/coilyco-flight-deck/ward/releases/download/$tag/ward-linux-$(arch)"
  log "downloading ward $tag for linux-$(arch)"
  curl -fsSL -H "Authorization: token ${FORGEJO_TOKEN:-}" -o /usr/local/bin/ward "$asset" \
    || die "download failed: $asset"
  chmod 0755 /usr/local/bin/ward
}

install_ward() {
  if [ -n "${WARD_FROM_SOURCE:-}" ]; then install_ward_from_source; else install_ward_from_release; fi
  ln -sf ward /usr/local/bin/warded
  ward version >&2 || die "ward did not install correctly"
}

arch() { case "$(uname -m)" in x86_64) echo amd64 ;; aarch64|arm64) echo arm64 ;; *) die "unsupported arch $(uname -m)" ;; esac; }
die() { log "fatal: $*"; exit 1; }

main() {
  install_ward
  exec ward container bootstrap "$@"
}

main "$@"
