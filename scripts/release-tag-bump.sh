#!/usr/bin/env bash
set -euo pipefail

latest_tag=$(git tag --list 'v[0-9]*' --sort=-v:refname | head -1)
if [ -z "${latest_tag:-}" ]; then
  echo "::error::no release tags found; cannot compute the next release tag" >&2
  exit 1
fi

version=${latest_tag#v}
major=${version%%.*}
rest=${version#*.}
minor=${rest%%.*}
patch=${version##*.}
if ! patch=$((patch + 1)); then
  echo "::error::could not increment patch version from ${latest_tag}" >&2
  exit 1
fi

new_tag="v${major}.${minor}.${patch}"
new_version="${new_tag#v}"
if changelog=$(git log --pretty=format:'%h%x09%s' "${latest_tag}..HEAD"); then
  :
else
  echo "::error::could not build changelog for ${latest_tag}..HEAD" >&2
  exit 1
fi

if [ -n "${GITHUB_OUTPUT:-}" ]; then
  {
    echo "previous_tag=${latest_tag}"
    echo "new_tag=${new_tag}"
    echo "new_version=${new_version}"
    echo "changelog<<WARD_RELEASE_TAG_BUMP_EOF"
    echo "${changelog}"
    echo "WARD_RELEASE_TAG_BUMP_EOF"
  } >> "$GITHUB_OUTPUT"
else
  printf 'previous_tag=%s\n' "${latest_tag}"
  printf 'new_tag=%s\n' "${new_tag}"
  printf 'new_version=%s\n' "${new_version}"
  printf '%s\n' "${changelog}"
fi
