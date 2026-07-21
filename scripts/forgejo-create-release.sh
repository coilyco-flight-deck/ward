#!/usr/bin/env bash
set -euo pipefail

: "${FORGEJO_API:?missing FORGEJO_API}"
: "${RELEASE_TAG:?missing RELEASE_TAG}"
: "${TOKEN:?missing TOKEN}"
: "${RELEASE_BODY:=}"
: "${RELEASE_NAME:=${RELEASE_TAG}}"
: "${RELEASE_DRAFT:=true}"

json_id() {
  python3 -c 'import json,sys; sys.stdout.write(str(json.load(sys.stdin).get("id") or ""))'
}

release_json=""
create_json=""
cleanup() {
  [ -n "$release_json" ] && rm -f "$release_json"
  [ -n "$create_json" ] && rm -f "$create_json"
}
trap cleanup EXIT

release_json=$(mktemp)
release_code=$(curl -sS -o "$release_json" -w '%{http_code}' \
  -H "Authorization: token ${TOKEN}" \
  "${FORGEJO_API}/releases/tags/${RELEASE_TAG}")

case "$release_code" in
  200)
    release_id=$(json_id < "$release_json")
    ;;
  404)
    release_id=""
    ;;
  *)
    echo "::error::could not resolve release ${RELEASE_TAG} (HTTP ${release_code})" >&2
    cat "$release_json" >&2 || true
    exit 1
    ;;
esac

payload=$(RELEASE_TAG="$RELEASE_TAG" RELEASE_NAME="$RELEASE_NAME" RELEASE_BODY="$RELEASE_BODY" RELEASE_DRAFT="$RELEASE_DRAFT" python3 -c '
import json, os, sys
sys.stdout.write(json.dumps({
    "tag_name": os.environ["RELEASE_TAG"],
    "name": os.environ["RELEASE_NAME"],
    "body": os.environ["RELEASE_BODY"],
    "draft": os.environ["RELEASE_DRAFT"] != "false",
}, separators=(",", ":")))
')

if [ -z "${release_id:-}" ]; then
  create_json=$(mktemp)
  create_code=$(curl -sS -o "$create_json" -w '%{http_code}' \
    -X POST -H "Authorization: token ${TOKEN}" \
    -H "Content-Type: application/json" \
    --data-binary "$payload" \
    "${FORGEJO_API}/releases")
  case "$create_code" in
    201)
      release_id=$(json_id < "$create_json")
      ;;
    409)
      release_json=$(mktemp)
      release_id=$(curl -fsSL -H "Authorization: token ${TOKEN}" \
        "${FORGEJO_API}/releases/tags/${RELEASE_TAG}" | json_id)
      ;;
    *)
      echo "::error::could not create release ${RELEASE_TAG} (HTTP ${create_code})" >&2
      cat "$create_json" >&2 || true
      exit 1
      ;;
  esac
fi

if [ -z "${release_id:-}" ]; then
  echo "::error::could not resolve release ${RELEASE_TAG}" >&2
  exit 1
fi

curl -fsSL -X PATCH -H "Authorization: token ${TOKEN}" \
  -H "Content-Type: application/json" \
  --data-binary "$payload" \
  "${FORGEJO_API}/releases/${release_id}" >/dev/null

echo "upserted draft release ${RELEASE_TAG}"
