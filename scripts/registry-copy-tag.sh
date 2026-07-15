#!/usr/bin/env bash
set -euo pipefail

: "${SOURCE_IMAGE:?missing SOURCE_IMAGE}"
: "${TARGET_IMAGE:?missing TARGET_IMAGE}"

REGISTRY_SCHEME="${REGISTRY_SCHEME:-https}"
SOURCE_USER="${SOURCE_USER:-${REGISTRY_USER:-oauth2}}"
TARGET_USER="${TARGET_USER:-${REGISTRY_USER:-oauth2}}"
SOURCE_TOKEN="${SOURCE_TOKEN:-${TOKEN:-}}"
TARGET_TOKEN="${TARGET_TOKEN:-${TOKEN:-}}"

: "${SOURCE_USER:?missing SOURCE_USER or REGISTRY_USER}"
: "${TARGET_USER:?missing TARGET_USER or REGISTRY_USER}"
: "${SOURCE_TOKEN:?missing SOURCE_TOKEN or TOKEN}"
: "${TARGET_TOKEN:?missing TARGET_TOKEN or TOKEN}"

accept_header='application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json'

parse_image_ref() {
  local ref="$1"
  local repo_with_host host repo tag

  case "$ref" in
    *@*)
      echo "::error::digest references are not supported: ${ref}" >&2
      exit 1
      ;;
  esac

  tag="${ref##*:}"
  repo_with_host="${ref%:*}"
  host="${repo_with_host%%/*}"
  repo="${repo_with_host#*/}"

  if [ -z "$host" ] || [ -z "$repo" ] || [ "$host" = "$repo_with_host" ]; then
    echo "::error::invalid image reference ${ref}; expected host/path:tag" >&2
    exit 1
  fi

  printf '%s\t%s\t%s\n' "$host" "$repo" "$tag"
}

read -r source_host source_repo source_tag <<EOF
$(parse_image_ref "$SOURCE_IMAGE")
EOF
read -r target_host target_repo target_tag <<EOF
$(parse_image_ref "$TARGET_IMAGE")
EOF

if [ "$source_host" != "$target_host" ]; then
  echo "::error::source and target registry hosts must match (${source_host} != ${target_host})" >&2
  exit 1
fi

fetch_url="${REGISTRY_SCHEME}://${source_host}/v2/${source_repo}/manifests/${source_tag}"
push_url="${REGISTRY_SCHEME}://${target_host}/v2/${target_repo}/manifests/${target_tag}"

source_headers=$(mktemp)
source_body=$(mktemp)
target_body=$(mktemp)
cleanup() {
  rm -f "$source_headers" "$source_body" "$target_body"
}
trap cleanup EXIT

source_code=$(curl -sS -o "$source_body" -D "$source_headers" -w '%{http_code}' \
  -u "${SOURCE_USER}:${SOURCE_TOKEN}" \
  -H "Accept: ${accept_header}" \
  "$fetch_url")
if [ "$source_code" != "200" ]; then
  echo "::error::could not fetch source manifest ${SOURCE_IMAGE} (HTTP ${source_code})" >&2
  cat "$source_body" >&2 || true
  exit 1
fi

content_type=$(awk 'BEGIN{IGNORECASE=1} /^Content-Type:/ {sub("\r$", "", $0); sub(/^Content-Type:[[:space:]]*/, "", $0); print; exit}' "$source_headers")
if [ -z "${content_type:-}" ]; then
  content_type='application/vnd.docker.distribution.manifest.v2+json'
fi

push_code=$(curl -sS -o "$target_body" -w '%{http_code}' \
  -X PUT \
  -u "${TARGET_USER}:${TARGET_TOKEN}" \
  -H "Content-Type: ${content_type}" \
  --data-binary @"$source_body" \
  "$push_url")
case "$push_code" in
  200|201|202)
    ;;
  *)
    echo "::error::could not publish target manifest ${TARGET_IMAGE} (HTTP ${push_code})" >&2
    cat "$target_body" >&2 || true
    exit 1
    ;;
esac

verify_body=$(mktemp)
trap 'rm -f "$source_headers" "$source_body" "$target_body" "$verify_body"' EXIT
verify_code=$(curl -sS -o "$verify_body" -w '%{http_code}' \
  -u "${TARGET_USER}:${TARGET_TOKEN}" \
  -H "Accept: ${accept_header}" \
  "$push_url")
if [ "$verify_code" != "200" ]; then
  echo "::error::published target manifest ${TARGET_IMAGE} did not resolve (HTTP ${verify_code})" >&2
  cat "$verify_body" >&2 || true
  exit 1
fi

if ! cmp -s "$source_body" "$verify_body"; then
  echo "::error::published target manifest ${TARGET_IMAGE} did not match source ${SOURCE_IMAGE}" >&2
  exit 1
fi

echo "published ${TARGET_IMAGE} from ${SOURCE_IMAGE} without a Docker daemon"
