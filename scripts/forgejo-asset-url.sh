#!/usr/bin/env bash
set -euo pipefail

# Resolve a release asset to the URL that serves its actual bytes, and print
# "<url> <sha256>" of that body. Forgejo's releases/download route returns
# tiny attachment-metadata JSON regardless of the Accept header, and the
# release API's assets[].browser_download_url just points back at that route;
# only the metadata's own browser_download_url (/attachments/<uuid>) serves
# bytes (ward#1493). This helper follows metadata hops until the body stops
# being attachment metadata, so it also works unchanged on any Forgejo that
# serves bytes from the download route directly.

: "${DOWNLOAD_BASE:?missing DOWNLOAD_BASE (e.g. https://host/owner/repo/releases/download/vX.Y.Z)}"
: "${ASSET_NAME:?missing ASSET_NAME}"

tmp="$(mktemp)"
cleanup() { rm -f "$tmp"; }
trap cleanup EXIT

url="${DOWNLOAD_BASE}/${ASSET_NAME}"
for _ in 1 2 3 4 5; do
  if ! curl -fsSL -o "$tmp" "$url"; then
    echo "::error::could not fetch ${ASSET_NAME} from ${url}" >&2
    exit 1
  fi
  next=$(python3 -c '
import json, sys
try:
    doc = json.load(open(sys.argv[1]))
except Exception:
    doc = None
if isinstance(doc, dict):
    sys.stdout.write(str(doc.get("browser_download_url") or ""))
' "$tmp")
  if [ -z "${next:-}" ]; then
    if command -v sha256sum >/dev/null 2>&1; then
      sha=$(sha256sum "$tmp" | awk '{print $1}')
    else
      sha=$(shasum -a 256 "$tmp" | awk '{print $1}')
    fi
    printf '%s %s\n' "$url" "$sha"
    exit 0
  fi
  url="$next"
done

echo "::error::${ASSET_NAME} did not resolve to a byte-serving URL after 5 metadata hops" >&2
exit 1
