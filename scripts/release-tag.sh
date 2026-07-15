#!/usr/bin/env bash
set -euo pipefail

match='v[0-9]*.[0-9]*.0'
tag_push_token="${TAG_PUSH_TOKEN:-}"

emit_output() {
  if [ -z "${GITHUB_OUTPUT:-}" ]; then
    return 0
  fi
  case "$1" in
    changelog)
      {
        printf '%s<<WARD_RELEASE_TAG_EOF\n' "$1"
        printf '%s\n' "$2"
        printf 'WARD_RELEASE_TAG_EOF\n'
      } >> "$GITHUB_OUTPUT"
      ;;
    *)
      printf '%s=%s\n' "$1" "$2" >> "$GITHUB_OUTPUT"
      ;;
  esac
}

latest_semver_tag() {
  git tag --list "$match" --sort=-version:refname | head -1 || true
}

current_tag=$(git tag --points-at HEAD --list "$match" --sort=-version:refname | head -1 || true)
if [ -n "$current_tag" ]; then
  new_tag="$current_tag"
else
  previous_tag=$(latest_semver_tag)
  if [ -z "$previous_tag" ]; then
    new_tag="v0.1.0"
  else
    semver=${previous_tag#v}
    IFS=. read -r major minor patch <<EOF
$semver
EOF
    new_tag="v${major}.$((minor + 1)).0"
  fi
fi

if [ -z "${previous_tag:-}" ]; then
  previous_tag=$(git tag --list "$match" --sort=-version:refname | grep -vxF "$new_tag" | head -1 || true)
fi

if [ -n "${previous_tag:-}" ]; then
  range="${previous_tag}..HEAD"
else
  range="HEAD"
fi

if [ -n "$current_tag" ] && [ "$current_tag" != "$new_tag" ]; then
  echo "::error::release-tag helper chose mismatched tag ${new_tag} for HEAD tag ${current_tag}" >&2
  exit 1
fi

if [ -z "$current_tag" ]; then
  git tag "$new_tag" HEAD
  remote_url=$(git remote get-url origin 2>/dev/null || true)
  if [ -n "$remote_url" ]; then
    if [ -z "$tag_push_token" ]; then
      echo "::error::missing TAG_PUSH_TOKEN to push release tag ${new_tag} to origin" >&2
      exit 1
    fi
    case "$remote_url" in
      https://*)
        auth_url="https://oauth2:${tag_push_token}@${remote_url#https://}"
        ;;
      *)
        echo "::error::unsupported origin url for release tag push: ${remote_url}" >&2
        exit 1
        ;;
    esac
    if ! git push "$auth_url" "refs/tags/${new_tag}" >/dev/null 2>&1; then
      if ! git ls-remote "$auth_url" "refs/tags/${new_tag}" | grep -q "refs/tags/${new_tag}$"; then
        echo "::error::could not push release tag ${new_tag} to origin" >&2
        exit 1
      fi
    fi
  fi
fi

new_version=${new_tag#v}
changelog=$(git log --pretty=format:'%h%x09%s' "$range")

emit_output previous_tag "${previous_tag:-}"
emit_output new_tag "$new_tag"
emit_output new_version "$new_version"
emit_output changelog "$changelog"

printf '%s\n' "$new_tag"
