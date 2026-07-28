#!/usr/bin/env bash
set -euo pipefail

: "${FORGEJO_API:?missing FORGEJO_API}"
: "${DRAFT_TAG:?missing DRAFT_TAG}"
: "${TOKEN:?missing TOKEN}"
: "${DIST_DIR:=dist}"
: "${MIN_PLATFORM_BYTES:=1048576}"

json_id() {
  python3 -c 'import json,sys; sys.stdout.write(str(json.load(sys.stdin).get("id") or ""))'
}

fetch_asset() {
  local asset_name=$1
  local dest=$2
  RELEASE_TAG="$DRAFT_TAG" ASSET_NAME="$asset_name" \
    FORGEJO_API="$FORGEJO_API" TOKEN="$TOKEN" \
    bash scripts/forgejo-release-asset.sh > "$dest"
}

release_json=$(mktemp)
assets_json=$(mktemp)
work_dir=$(mktemp -d)
stage_dir="$work_dir/dist"
cleanup() {
  rm -f "$release_json" "$assets_json"
  rm -rf "$work_dir"
}
trap cleanup EXIT

release_code=$(curl -sS -o "$release_json" -w '%{http_code}' \
  -H "Authorization: token ${TOKEN}" \
  "${FORGEJO_API}/releases/tags/${DRAFT_TAG}")
case "$release_code" in
  200)
    release_id=$(json_id < "$release_json")
    ;;
  *)
    echo "::error::could not resolve draft release ${DRAFT_TAG} (HTTP ${release_code})" >&2
    cat "$release_json" >&2 || true
    exit 1
    ;;
esac

if [ -z "${release_id:-}" ]; then
  echo "::error::could not resolve draft release ${DRAFT_TAG}" >&2
  exit 1
fi

curl -fsSL -H "Authorization: token ${TOKEN}" \
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
    print(f"{asset.get('id') or ''}\t{name}")
PY
)

sha256_body="$work_dir/SHA256SUMS"
fetch_asset SHA256SUMS "$sha256_body"

python3 - "$sha256_body" <<'PY'
import sys
from pathlib import Path

body = Path(sys.argv[1]).read_text(encoding="utf-8")
if not body.endswith("\n"):
    print("SHA256SUMS is missing a trailing newline", file=sys.stderr)
    raise SystemExit(1)
PY

declare -A expected_hashes=()
declare -A expected_names=()
expected_names[SHA256SUMS]=1

while IFS="$(printf '\t')" read -r asset_id asset_name; do
  [ -n "${asset_id:-}" ] || continue
  expected_names["$asset_name"]=1
done <<EOF
$asset_rows
EOF

while IFS= read -r line; do
  [ -n "$line" ] || continue
  hash=${line%% *}
  rest=${line#*  }
  asset=$rest
  if [ -z "$hash" ] || [ -z "$asset" ] || [ "$rest" = "$line" ]; then
    echo "::error::invalid SHA256SUMS line: ${line}" >&2
    exit 1
  fi
  expected_hashes["$asset"]="$hash"
  expected_names["$asset"]=1
  expected_names["$asset.sha256"]=1
done < "$sha256_body"

mkdir -p "$stage_dir"

for name in "${!expected_names[@]}"; do
  if ! printf '%s\n' "$asset_rows" | awk -F '\t' -v n="$name" '$2 == n { found = 1 } END { exit(found ? 0 : 1) }'; then
    echo "::error::draft release ${DRAFT_TAG} is missing asset ${name}" >&2
    exit 1
  fi
done

while IFS="$(printf '\t')" read -r asset_id asset_name; do
  [ -n "${asset_id:-}" ] || continue
  case "$asset_name" in
    SHA256SUMS)
      cp "$sha256_body" "$stage_dir/$asset_name"
      ;;
    *.sha256)
      base=${asset_name%.sha256}
      expected=${expected_hashes[$base]:-}
      if [ -z "$expected" ]; then
        echo "::error::draft release ${DRAFT_TAG} contains unexpected checksum sidecar ${asset_name}" >&2
        exit 1
      fi
      tmp="$work_dir/$asset_name"
      fetch_asset "$asset_name" "$tmp"
      actual=$(tr -d '[:space:]' < "$tmp")
      if [ "$actual" != "$expected" ]; then
        echo "::error::checksum sidecar ${asset_name} does not match ${base}" >&2
        exit 1
      fi
      cp "$tmp" "$stage_dir/$asset_name"
      ;;
    *)
      expected=${expected_hashes[$asset_name]:-}
      if [ -z "$expected" ]; then
        echo "::error::draft release ${DRAFT_TAG} contains unexpected asset ${asset_name}" >&2
        exit 1
      fi
      tmp="$work_dir/$asset_name"
      fetch_asset "$asset_name" "$tmp"
      actual=$(sha256sum "$tmp" | awk '{print $1}')
      if [ "$actual" != "$expected" ]; then
        echo "::error::checksum mismatch for ${asset_name}: got ${actual}, want ${expected}" >&2
        exit 1
      fi
      size=$(wc -c < "$tmp" | tr -d '[:space:]')
      if [ "$size" -lt "$MIN_PLATFORM_BYTES" ]; then
        echo "::error::platform binary ${asset_name} is implausibly small (${size} bytes)" >&2
        exit 1
      fi
      cp "$tmp" "$stage_dir/$asset_name"
      ;;
  esac
done <<EOF
$asset_rows
EOF

for name in "${!expected_names[@]}"; do
  if [ ! -e "$stage_dir/$name" ]; then
    echo "::error::failed to write verified asset ${name} to staged dist" >&2
    exit 1
  fi
done

rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"
cp -a "$stage_dir"/. "$DIST_DIR"/

echo "verified draft release ${DRAFT_TAG} assets into ${DIST_DIR}"
