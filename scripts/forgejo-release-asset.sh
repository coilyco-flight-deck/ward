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
  python3 -c 'import json,sys; sys.stdout.write(str(json.load(sys.stdin).get("id") or ""))'
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
asset_id=$(printf '%s' "$assets" | ASSET_NAME="$ASSET_NAME" python3 -c '
import json, os, sys
assets = json.loads(sys.stdin.read() or "[]") or []
match = next((x for x in assets if x.get("name") == os.environ["ASSET_NAME"]), None)
if match:
    sys.stdout.write(str(match["id"]))
')
if [ -z "${asset_id:-}" ]; then
  echo "::error::release ${RELEASE_TAG} does not have asset ${ASSET_NAME}" >&2
  exit 1
fi

asset_json=$(mktemp)
cleanup_json() {
  [ -n "${asset_json:-}" ] && rm -f "$asset_json"
}
trap 'cleanup_json; cleanup' EXIT

url="${FORGEJO_API}/releases/${release_id}/assets/${asset_id}"
for _ in 1 2 3 4 5; do
  asset_code=$(curl -sS -o "$asset_json" -w '%{http_code}' \
    "${auth_args[@]}" \
    -H "Accept: application/octet-stream" \
    "$url")
  case "$asset_code" in
    200)
      # Follow Forgejo metadata hops. Non-metadata bodies pass through raw,
      # and callers validate their expected shape (ward#1493).
      next_url=$(python3 -c '
import json, sys
try:
    sys.stdout.write(str(json.load(sys.stdin).get("browser_download_url") or ""))
except Exception:
    sys.stdout.write("")
' < "$asset_json")
      if [ -z "${next_url:-}" ]; then
        cat "$asset_json"
        exit 0
      fi
      url="$next_url"
      ;;
    *)
      echo "::error::could not fetch release asset ${ASSET_NAME} (HTTP ${asset_code})" >&2
      cat "$asset_json" >&2 || true
      exit 1
      ;;
  esac
done

echo "::error::release ${RELEASE_TAG} asset ${ASSET_NAME} did not resolve to a raw body after 5 hops" >&2
cat "$asset_json" >&2 || true
exit 1
