#!/usr/bin/env bash
set -euo pipefail

: "${DRAFT_TAG:?missing DRAFT_TAG}"
: "${FORGEJO_API:?missing FORGEJO_API}"
: "${TOKEN:?missing TOKEN}"
: "${DIST_DIR:=dist}"
: "${RELEASE_NAME:=${DRAFT_TAG}}"

rel_json=""
create_json=""
rel_id=""
cleanup() {
  [ -n "$rel_json" ] && rm -f "$rel_json"
  [ -n "$create_json" ] && rm -f "$create_json"
}
trap cleanup EXIT

json_id() {
  python3 -c 'import json,sys; sys.stdout.write(str(json.load(sys.stdin).get("id") or ""))'
}

resolve_draft_release_id() {
  local rel_code payload create_code
  rel_json=$(mktemp)
  create_json=$(mktemp)

  rel_code=$(curl -sS -o "$rel_json" -w '%{http_code}' \
    -H "Authorization: token ${TOKEN}" \
    "${FORGEJO_API}/releases/tags/${DRAFT_TAG}")
  case "$rel_code" in
    200)
      rel_id=$(json_id < "$rel_json")
      ;;
    404)
      rel_id=""
      ;;
    *)
      echo "::error::could not resolve draft release ${DRAFT_TAG} (HTTP ${rel_code})" >&2
      cat "$rel_json" >&2 || true
      exit 1
      ;;
  esac

  if [ -z "${rel_id:-}" ]; then
    payload=$(DRAFT_TAG="$DRAFT_TAG" RELEASE_NAME="$RELEASE_NAME" RELEASE_BODY="${RELEASE_BODY:-}" python3 -c '
import json, os, sys
sys.stdout.write(json.dumps({
    "tag_name": os.environ["DRAFT_TAG"],
    "name": os.environ["RELEASE_NAME"],
    "draft": True,
    "body": os.environ.get("RELEASE_BODY", ""),
}, separators=(",", ":")))
')
    create_code=$(curl -sS -o "$create_json" -w '%{http_code}' \
      -X POST -H "Authorization: token ${TOKEN}" \
      -H "Content-Type: application/json" \
      --data-binary "$payload" \
      "${FORGEJO_API}/releases")
    case "$create_code" in
      201)
        rel_id=$(json_id < "$create_json")
        ;;
      409)
        rel_id=$(curl -fsSL -H "Authorization: token ${TOKEN}" \
          "${FORGEJO_API}/releases/tags/${DRAFT_TAG}" \
          | json_id)
        ;;
      *)
        echo "::error::could not create draft release ${DRAFT_TAG} (HTTP ${create_code})" >&2
        cat "$create_json" >&2 || true
        exit 1
        ;;
    esac
  fi

  if [ -z "${rel_id:-}" ]; then
    echo "::error::could not resolve draft release ${DRAFT_TAG}" >&2
    exit 1
  fi
}

resolve_draft_release_id

if [ -d "$DIST_DIR" ]; then
  existing=$(curl -fsSL -H "Authorization: token ${TOKEN}" \
    "${FORGEJO_API}/releases/${rel_id}/assets?per_page=100" || echo '[]')

  for name in $(cd "$DIST_DIR" && ls); do
    old=$(printf '%s' "$existing" | ASSET="$name" python3 -c '
import json, os, sys
assets = json.loads(sys.stdin.read() or "[]") or []
match = next((x for x in assets if x.get("name") == os.environ["ASSET"]), None)
if match:
    sys.stdout.write(str(match["id"]))
')
    if [ -n "$old" ]; then
      curl -fsSL -X DELETE -H "Authorization: token ${TOKEN}" \
        "${FORGEJO_API}/releases/${rel_id}/assets/${old}" || true
    fi
    curl -fsSL -X POST -H "Authorization: token ${TOKEN}" \
      -H "Content-Type: application/octet-stream" \
      --data-binary @"${DIST_DIR}/${name}" \
      "${FORGEJO_API}/releases/${rel_id}/assets?name=${name}"
    echo "uploaded ${name} to draft ${DRAFT_TAG}"
  done
fi

echo "published draft release ${DRAFT_TAG} with the full matrix + SHA256SUMS"
