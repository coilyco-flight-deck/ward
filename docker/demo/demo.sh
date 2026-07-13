#!/bin/sh

set -eu

fail() {
	printf 'ward demo image: %s\n' "$1" >&2
	exit 1
}

workspace=${WARD_DEMO_WORKSPACE:-/workspace/cli}
substrate=${WARD_DEMO_SUBSTRATE:-/substrate/cli}
repo=${WARD_DEMO_REPO:-cli/cli}
org=${WARD_DEMO_ORG:-cli}

[ -d "$workspace" ] || fail "workspace mount missing at $workspace"
[ -d "$substrate" ] || fail "substrate mount missing at $substrate"

printf 'ward demo image\n'
printf 'repo: %s\n' "$repo"
printf 'org: %s\n' "$org"
printf 'workspace: %s\n' "$workspace"
printf 'substrate: %s\n' "$substrate"

if command -v ward >/dev/null 2>&1; then
	printf '\nward:\n'
	ward version
fi

printf '\nworkspace:\n'
if git -C "$workspace" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
	git -C "$workspace" status --short --branch
else
	printf '(not a git checkout)\n'
fi

printf '\nREADME:\n'
if [ -f "$workspace/README.md" ]; then
	sed -n '1,12p' "$workspace/README.md"
else
	printf '(no README.md at %s)\n' "$workspace"
fi

printf '\nsubstrate entries:\n'
find "$substrate" -mindepth 1 -maxdepth 1 -type d | sort | sed 's#^#- #'
