#!/usr/bin/env bash
# Build ward-kdl and symlink it onto PATH as `ward-kdl-tmp` for ad hoc human testing.
# The -tmp suffix marks a throwaway test binary; re-run any time for a fresh build.
set -euo pipefail

# Resolve the repo root from this script's location, so it works from any checkout.
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEST="${WARD_KDL_TMP_DEST:-$HOME/.local/bin/ward-kdl-tmp}"

cd "$REPO"

# Generate the gitignored main.go from guardfile + spec lock, then build bin/ward-kdl.
# Needs the committed spec lock; run `make lock` first if it is missing.
make ward-kdl

mkdir -p "$(dirname "$DEST")"
ln -sf "$REPO/bin/ward-kdl" "$DEST"

echo "linked: $DEST -> $REPO/bin/ward-kdl"
command -v ward-kdl-tmp
