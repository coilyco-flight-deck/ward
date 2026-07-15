#!/usr/bin/env bash
set -euo pipefail

: "${FORGEJO_API:?missing FORGEJO_API}"
: "${RELEASE_TAG:?missing RELEASE_TAG}"
: "${ASSET_NAME:?missing ASSET_NAME}"

TOKEN="${TOKEN:-}"
auth_args=()
if [ -n "$TOKEN" ]; then
  auth_args=(-H "Authorization: token ${TOKEN}")
fi

json_id() {
  node -e 'process.stdout.write(String(JSON.parse(require("fs").readFileSync(0, "utf8")).id || ""))'
}

release_json=""
cleanup() {
  [ -n "$release_json" ] && rm -f "$release_json"
}
trap cleanup EXIT

release_json=$(mktemp)
release_code=$(curl -sS -o "$release_json" -w '%{http_code}' \
  "${auth_args[@]}" \
  "${FORGEJO_API}/releases/tags/${RELEASE_TAG}")
case "$release_code" in
  200)
    release_id=$(json_id < "$release_json")
    ;;
  *)
    echo "::error::could not resolve release ${RELEASE_TAG} (HTTP ${release_code})" >&2
    cat "$release_json" >&2 || true
    exit 1
    ;;
esac

if [ -z "${release_id:-}" ]; then
  echo "::error::could not resolve release ${RELEASE_TAG}" >&2
  exit 1
fi

assets=$(curl -fsSL \
  "${auth_args[@]}" \
  "${FORGEJO_API}/releases/${release_id}/assets?per_page=100")
asset_id=$(printf '%s' "$assets" | ASSET_NAME="$ASSET_NAME" node -e '
  const a = JSON.parse(require("fs").readFileSync(0, "utf8") || "[]");
  const m = (a || []).find(x => x.name === process.env.ASSET_NAME);
  if (m) process.stdout.write(String(m.id));
')
if [ -z "${asset_id:-}" ]; then
  echo "::error::release ${RELEASE_TAG} does not have asset ${ASSET_NAME}" >&2
  exit 1
fi

curl -fsSL \
  "${auth_args[@]}" \
  -H "Accept: application/octet-stream" \
  "${FORGEJO_API}/releases/${release_id}/assets/${asset_id}"
