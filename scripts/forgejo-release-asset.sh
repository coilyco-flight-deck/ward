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
      asset_body=$(cat "$asset_json")
      if printf '%s' "$asset_body" | grep -Eq '^[0-9a-f]{64}$'; then
        printf '%s' "$asset_body"
        exit 0
      fi
      next_url=$(node -e '
        const fs = require("fs");
        try {
          const body = fs.readFileSync(0, "utf8");
          const parsed = JSON.parse(body);
          process.stdout.write(String(parsed.browser_download_url || ""));
        } catch {
          process.stdout.write("");
        }
      ' < "$asset_json")
      if [ -z "${next_url:-}" ]; then
        echo "::error::release ${RELEASE_TAG} asset ${ASSET_NAME} did not return raw body or browser_download_url" >&2
        cat "$asset_json" >&2 || true
        exit 1
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

echo "::error::release ${RELEASE_TAG} asset ${ASSET_NAME} did not resolve to a raw digest after 5 hops" >&2
cat "$asset_json" >&2 || true
exit 1
