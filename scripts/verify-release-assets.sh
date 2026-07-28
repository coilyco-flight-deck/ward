#!/usr/bin/env bash
set -euo pipefail

: "${FORGEJO_API:?missing FORGEJO_API}"
: "${RELEASE_TAG:?missing RELEASE_TAG}"
: "${DIST_DIR:=dist}"

TOKEN="${TOKEN:-}"
auth_args=()
if [ -n "$TOKEN" ]; then
  auth_args=(-H "Authorization: token ${TOKEN}")
fi

json_id() {
  python3 -c 'import json,sys; sys.stdout.write(str(json.load(sys.stdin).get("id") or ""))'
}

fetch_asset() {
  local asset_name=$1
  local dest=$2
  RELEASE_TAG="$RELEASE_TAG" ASSET_NAME="$asset_name" \
    FORGEJO_API="$FORGEJO_API" TOKEN="$TOKEN" \
    bash scripts/forgejo-release-asset.sh > "$dest"
}

release_json=$(mktemp)
assets_json=$(mktemp)
tmp_dir=$(mktemp -d)
cleanup() {
  rm -f "$release_json" "$assets_json"
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

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

curl -fsSL \
  "${auth_args[@]}" \
  "${FORGEJO_API}/releases/${release_id}/assets?per_page=100" > "$assets_json"

asset_rows=$(
  python3 - "$assets_json" <<'PY'
import json, sys
assets = json.load(open(sys.argv[1])) or []
seen = set()
for asset in assets:
    name = str(asset.get("name") or "")
    if not name:
        print("missing asset name", file=sys.stderr)
        raise SystemExit(1)
    if name in seen:
        print(f"duplicate asset name {name}", file=sys.stderr)
        raise SystemExit(1)
    seen.add(name)
    print(name)
PY
)

declare -A remote_names=()
while IFS= read -r name; do
  [ -n "$name" ] || continue
  remote_names["$name"]=1
done <<EOF
$asset_rows
EOF

local_names=()
while IFS= read -r path; do
  [ -n "$path" ] || continue
  local_names+=("$(basename "$path")")
done < <(find "$DIST_DIR" -maxdepth 1 -type f | sort)

if [ "${#local_names[@]}" -eq 0 ]; then
  echo "::error::${DIST_DIR} does not contain verified release files" >&2
  exit 1
fi

declare -A local_seen=()
for name in "${local_names[@]}"; do
  if [ -n "${local_seen[$name]:-}" ]; then
    echo "::error::local dist contains duplicate file ${name}" >&2
    exit 1
  fi
  local_seen["$name"]=1
  if [ -z "${remote_names[$name]:-}" ]; then
    echo "::error::stable release ${RELEASE_TAG} is missing asset ${name}" >&2
    exit 1
  fi
done

for name in "${!remote_names[@]}"; do
  if [ -z "${local_seen[$name]:-}" ]; then
    echo "::error::stable release ${RELEASE_TAG} contains unexpected asset ${name}" >&2
    exit 1
  fi
done

for name in "${local_names[@]}"; do
  local_file="$DIST_DIR/$name"
  remote_file="$tmp_dir/$name"
  fetch_asset "$name" "$remote_file"
  local_size=$(wc -c < "$local_file" | tr -d '[:space:]')
  remote_size=$(wc -c < "$remote_file" | tr -d '[:space:]')
  if [ "$local_size" != "$remote_size" ]; then
    echo "::error::stable asset ${name} size mismatch: local ${local_size}, remote ${remote_size}" >&2
    exit 1
  fi
  local_hash=$(sha256sum "$local_file" | awk '{print $1}')
  remote_hash=$(sha256sum "$remote_file" | awk '{print $1}')
  if [ "$local_hash" != "$remote_hash" ]; then
    echo "::error::stable asset ${name} checksum mismatch" >&2
    exit 1
  fi
done

echo "verified stable release ${RELEASE_TAG} assets against ${DIST_DIR}"
