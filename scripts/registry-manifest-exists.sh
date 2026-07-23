#!/usr/bin/env bash
set -euo pipefail

# Check an OCI/Docker registry manifest directly. This keeps the release
# acceptance check independent of a Docker daemon in the Actions job container.

: "${IMAGE:?missing IMAGE}"

REGISTRY_SCHEME="${REGISTRY_SCHEME:-https}"
REGISTRY_USER="${REGISTRY_USER:-oauth2}"
REGISTRY_TOKEN="${REGISTRY_TOKEN:-${TOKEN:-}}"

: "${REGISTRY_TOKEN:?missing REGISTRY_TOKEN or TOKEN}"

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

read -r host repo tag <<EOF
$(parse_image_ref "$IMAGE")
EOF

accept_header='application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json'
body=$(mktemp)
cleanup() { rm -f "$body"; }
trap cleanup EXIT

code=$(curl -sS -o "$body" -w '%{http_code}' \
  -u "${REGISTRY_USER}:${REGISTRY_TOKEN}" \
  -H "Accept: ${accept_header}" \
  "${REGISTRY_SCHEME}://${host}/v2/${repo}/manifests/${tag}")
if [ "$code" != "200" ]; then
  echo "::error::default agent image ${IMAGE} does not resolve (HTTP ${code}); promote must publish it first" >&2
  cat "$body" >&2 || true
  exit 1
fi

echo "default agent image ${IMAGE} resolves"
