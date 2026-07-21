#!/usr/bin/env bash
set -euo pipefail

# Cross-repo registry retag without a Docker daemon. A manifest PUT alone is
# not enough across repositories: the registry validates that every referenced
# blob and child manifest already exists in the TARGET repo, so this script
# mounts blobs from the source repo (POST ?mount=&from=) and copies child
# manifests by digest before putting the final tag. Falls back to a
# download-and-upload round trip when cross-repo mounting is refused.

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

base_url="${REGISTRY_SCHEME}://${source_host}/v2"

workdir=$(mktemp -d)
cleanup() { rm -rf "$workdir"; }
trap cleanup EXIT

# fail_http <label> <code> <body-file>: print the registry's actual response
# so a failed run is diagnosable from the step log alone.
fail_http() {
  local label="$1" code="$2" body="$3"
  echo "::error::${label} (HTTP ${code})" >&2
  cat "$body" >&2 || true
  exit 1
}

# manifest_digests <manifest-file> <kind>: list referenced digests. kind is
# "blobs" (config + layers of an image manifest) or "children" (entries of an
# index / manifest list). Emits nothing when the field is absent.
manifest_digests() {
  python3 - "$1" "$2" <<'PY'
import json, sys
doc = json.load(open(sys.argv[1]))
kind = sys.argv[2]
if kind == "children":
    for m in doc.get("manifests", []):
        print(m["digest"])
else:
    cfg = doc.get("config", {}).get("digest")
    if cfg:
        print(cfg)
    for layer in doc.get("layers", []):
        print(layer["digest"])
PY
}

is_index() {
  case "$1" in
    *image.index*|*manifest.list*) return 0 ;;
    *) return 1 ;;
  esac
}

# fetch_manifest <ref> <out-file> <ct-out-file>: GET a manifest (tag or
# digest) from the source repo, saving body and content type.
fetch_manifest() {
  local ref="$1" out="$2" ct_out="$3" headers code
  headers="${workdir}/fetch-headers"
  code=$(curl -sS -o "$out" -D "$headers" -w '%{http_code}' \
    -u "${SOURCE_USER}:${SOURCE_TOKEN}" \
    -H "Accept: ${accept_header}" \
    "${base_url}/${source_repo}/manifests/${ref}")
  if [ "$code" != "200" ]; then
    fail_http "could not fetch source manifest ${source_repo}@${ref}" "$code" "$out"
  fi
  awk 'BEGIN{IGNORECASE=1} /^Content-Type:/ {sub("\r$", "", $0); sub(/^Content-Type:[[:space:]]*/, "", $0); print; exit}' \
    "$headers" > "$ct_out"
  if [ ! -s "$ct_out" ]; then
    echo 'application/vnd.docker.distribution.manifest.v2+json' > "$ct_out"
  fi
}

# ensure_blob <digest>: make the blob exist in the target repo. Mount from
# the source repo when the registry supports it, else round-trip the bytes.
ensure_blob() {
  local digest="$1" code body upload_headers location
  body="${workdir}/blob-body"

  code=$(curl -sS -o /dev/null -w '%{http_code}' -I \
    -u "${TARGET_USER}:${TARGET_TOKEN}" \
    "${base_url}/${target_repo}/blobs/${digest}")
  if [ "$code" = "200" ]; then
    return 0
  fi

  code=$(curl -sS -o "$body" -w '%{http_code}' -X POST \
    -u "${TARGET_USER}:${TARGET_TOKEN}" \
    "${base_url}/${target_repo}/blobs/uploads/?mount=${digest}&from=${source_repo}")
  if [ "$code" = "201" ]; then
    echo "mounted blob ${digest}"
    return 0
  fi
  echo "blob mount for ${digest} returned HTTP ${code}; falling back to copy" >&2

  code=$(curl -sSL -o "${workdir}/blob-data" -w '%{http_code}' \
    -u "${SOURCE_USER}:${SOURCE_TOKEN}" \
    "${base_url}/${source_repo}/blobs/${digest}")
  if [ "$code" != "200" ]; then
    fail_http "could not fetch source blob ${digest}" "$code" "${workdir}/blob-data"
  fi

  upload_headers="${workdir}/upload-headers"
  code=$(curl -sS -o "$body" -D "$upload_headers" -w '%{http_code}' -X POST \
    -u "${TARGET_USER}:${TARGET_TOKEN}" \
    "${base_url}/${target_repo}/blobs/uploads/")
  if [ "$code" != "202" ]; then
    fail_http "could not open blob upload for ${digest}" "$code" "$body"
  fi
  location=$(awk 'BEGIN{IGNORECASE=1} /^Location:/ {sub("\r$", "", $0); sub(/^Location:[[:space:]]*/, "", $0); print; exit}' "$upload_headers")
  if [ -z "$location" ]; then
    echo "::error::blob upload for ${digest} returned no Location header" >&2
    exit 1
  fi
  case "$location" in
    http://*|https://*) ;;
    *) location="${REGISTRY_SCHEME}://${source_host}${location}" ;;
  esac
  case "$location" in
    *\?*) location="${location}&digest=${digest}" ;;
    *) location="${location}?digest=${digest}" ;;
  esac
  code=$(curl -sS -o "$body" -w '%{http_code}' -X PUT \
    -u "${TARGET_USER}:${TARGET_TOKEN}" \
    -H "Content-Type: application/octet-stream" \
    --data-binary @"${workdir}/blob-data" \
    "$location")
  if [ "$code" != "201" ]; then
    fail_http "could not upload blob ${digest}" "$code" "$body"
  fi
  echo "uploaded blob ${digest}"
}

# put_manifest <ref> <manifest-file> <content-type>
put_manifest() {
  local ref="$1" file="$2" ct="$3" code body
  body="${workdir}/put-body"
  code=$(curl -sS -o "$body" -w '%{http_code}' -X PUT \
    -u "${TARGET_USER}:${TARGET_TOKEN}" \
    -H "Content-Type: ${ct}" \
    --data-binary @"$file" \
    "${base_url}/${target_repo}/manifests/${ref}")
  case "$code" in
    200|201|202) ;;
    *) fail_http "could not publish target manifest ${target_repo}@${ref}" "$code" "$body" ;;
  esac
}

# copy_manifest <ref> <final-ref>: copy one manifest (and everything it
# references) from source repo to target repo, publishing it as final-ref.
# Recurses one level per index nesting.
copy_manifest() {
  local ref="$1" final_ref="$2" file ct_file ct digest
  file=$(mktemp "${workdir}/manifest-XXXXXX")
  ct_file="${file}.ct"
  fetch_manifest "$ref" "$file" "$ct_file"
  ct=$(cat "$ct_file")

  if is_index "$ct"; then
    while IFS= read -r digest; do
      [ -n "$digest" ] || continue
      copy_manifest "$digest" "$digest"
    done <<EOF
$(manifest_digests "$file" children)
EOF
  else
    while IFS= read -r digest; do
      [ -n "$digest" ] || continue
      ensure_blob "$digest"
    done <<EOF
$(manifest_digests "$file" blobs)
EOF
  fi

  put_manifest "$final_ref" "$file" "$ct"
}

copy_manifest "$source_tag" "$target_tag"

verify_body="${workdir}/verify-body"
verify_code=$(curl -sS -o "$verify_body" -w '%{http_code}' \
  -u "${TARGET_USER}:${TARGET_TOKEN}" \
  -H "Accept: ${accept_header}" \
  "${base_url}/${target_repo}/manifests/${target_tag}")
if [ "$verify_code" != "200" ]; then
  fail_http "published target manifest ${TARGET_IMAGE} did not resolve" "$verify_code" "$verify_body"
fi

echo "published ${TARGET_IMAGE} from ${SOURCE_IMAGE} without a Docker daemon"
