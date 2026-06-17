#!/usr/bin/env bash
# ward container entrypoint. Bind-mounted into the aos dev-base image at
# /opt/ward/entrypoint.sh by `ward container up` (it is embedded in the ward
# binary, not baked into the image). Responsibilities, in order: install ward,
# configure forgejo git auth, cached-fresh-clone the target repo, compose the
# per-mode operating context, then exec the agent. See ward docs/container.md.
set -euo pipefail

log() { printf 'ward-container: %s\n' "$*" >&2; }
die() { log "fatal: $*"; exit 1; }

: "${WARD_TARGET_OWNER:?missing WARD_TARGET_OWNER}"
: "${WARD_TARGET_NAME:?missing WARD_TARGET_NAME}"
: "${WARD_FORGEJO_BASE:?missing WARD_FORGEJO_BASE}"
WARD_MODE="${WARD_MODE:-claude}"
WARD_AGENT="${WARD_AGENT:-claude}"
WARD_CONTEXT_LEVEL="${WARD_CONTEXT_LEVEL:-2}"
WARD_GITCACHE="${WARD_GITCACHE:-/gitcache}"
WARD_CONTEXT_SRC="${WARD_CONTEXT_SRC:-/opt/ward-context}"
GIT_USER_NAME="${WARD_GIT_NAME:-ward-container}"
GIT_USER_EMAIL="${WARD_GIT_EMAIL:-coilysiren@gmail.com}"

forgejo_host="$(printf '%s' "$WARD_FORGEJO_BASE" | sed -E 's#^https?://##; s#/.*$##')"

# --- forgejo git auth (token rides --env-file, never argv) -------------------
configure_git_auth() {
  git config --global user.name "$GIT_USER_NAME"
  git config --global user.email "$GIT_USER_EMAIL"
  git config --global init.defaultBranch main
  if [ -n "${FORGEJO_TOKEN:-}" ]; then
    git config --global credential.helper store
    printf 'https://%s:%s@%s\n' coilysiren "$FORGEJO_TOKEN" "$forgejo_host" > "$HOME/.git-credentials"
    chmod 600 "$HOME/.git-credentials"
  else
    log "no FORGEJO_TOKEN: clone/push will only work for anonymous repos"
  fi
}

# --- install ward ------------------------------------------------------------
arch() { case "$(uname -m)" in x86_64) echo amd64 ;; aarch64|arm64) echo arm64 ;; *) die "unsupported arch $(uname -m)" ;; esac; }

install_ward_from_source() {
  log "building ward from mounted source $WARD_FROM_SOURCE"
  ( cd "$WARD_FROM_SOURCE" \
    && GOPROXY=direct GOSUMDB=off go build -o /usr/local/bin/ward ./cmd/ward )
}

resolve_ward_tag() {
  local tag="${WARD_VERSION:-}"
  if [ -z "$tag" ] || [ "$tag" = "dev" ]; then
    tag="$(curl -fsSL -H "Authorization: token ${FORGEJO_TOKEN:-}" \
      "$WARD_FORGEJO_BASE/api/v1/repos/coilyco-flight-deck/ward/releases/latest" \
      | jq -r '.tag_name')" || tag=""
  fi
  printf '%s' "$tag"
}

install_ward_from_release() {
  local tag asset
  tag="$(resolve_ward_tag)"
  [ -n "$tag" ] && [ "$tag" != "null" ] || die "could not resolve a ward release tag (set --ward-source to build instead)"
  asset="$WARD_FORGEJO_BASE/coilyco-flight-deck/ward/releases/download/$tag/ward-linux-$(arch)"
  log "downloading ward $tag for linux-$(arch)"
  curl -fsSL -H "Authorization: token ${FORGEJO_TOKEN:-}" -o /usr/local/bin/ward "$asset" \
    || die "download failed: $asset"
  chmod 0755 /usr/local/bin/ward
}

install_ward() {
  if [ -n "${WARD_FROM_SOURCE:-}" ]; then install_ward_from_source; else install_ward_from_release; fi
  ward version >&2 || die "ward did not install correctly"
}

# --- cached fresh clone (mirror in the shared gitcache volume) ---------------
clone_target() {
  local mirror="$WARD_GITCACHE/${WARD_MIRROR_NAME}"
  local url="$WARD_FORGEJO_BASE/$WARD_TARGET_OWNER/$WARD_TARGET_NAME.git"
  mkdir -p "$WARD_GITCACHE"
  if [ -d "$mirror" ]; then
    log "refreshing cached mirror $mirror"
    git -C "$mirror" remote update --prune >&2 || log "mirror refresh failed, using cached state"
  else
    log "cloning mirror (first time) $url"
    git clone --mirror "$url" "$mirror" >&2
  fi
  local work="/workspace/$WARD_TARGET_NAME"
  rm -rf "$work"
  git clone "$mirror" "$work" >&2
  git -C "$work" remote set-url origin "$url"
  git -C "$work" config push.default current
  if [ -n "${WARD_BRANCH:-}" ]; then
    git -C "$work" checkout -B "$WARD_BRANCH" >&2
  fi
  printf '%s' "$work"
}

# --- compose per-mode operating context (the least-context ladder) -----------
# Levels: 2=doctrine+host context, 1=doctrine+host AGENTS.md, 0=doctrine only.
compose_context() {
  local out="$HOME/.claude/CLAUDE.md"
  mkdir -p "$(dirname "$out")"
  cat /opt/ward/AGENTS.container.md > "$out"
  if [ "$WARD_CONTEXT_LEVEL" -ge 2 ] && [ -d "$WARD_CONTEXT_SRC" ]; then
    for f in CLAUDE.md AGENTS.md; do
      [ -f "$WARD_CONTEXT_SRC/$f" ] && { printf '\n\n---\n\n' >> "$out"; cat "$WARD_CONTEXT_SRC/$f" >> "$out"; }
    done
  elif [ "$WARD_CONTEXT_LEVEL" -eq 1 ] && [ -f "$WARD_CONTEXT_SRC/AGENTS.md" ]; then
    printf '\n\n---\n\n' >> "$out"; cat "$WARD_CONTEXT_SRC/AGENTS.md" >> "$out"
  fi
  log "composed context (level $WARD_CONTEXT_LEVEL) at $out"
}

# --- reaper: deterministic teardown backstop (docs/container-reap.md) --------
# Static ward code lands/salvages residual work on any agent exit; nothing lost.
reap() {
  trap - EXIT
  [ -n "${WARD_REAP_WORK:-}" ] || return 0
  log "reaping: salvage residual work before teardown"
  ward container reap --work "$WARD_REAP_WORK" \
    || log "reaper returned non-zero; check this log for an UNPRESERVED PATCH block before 'ward container down'"
}

# --- launch ------------------------------------------------------------------
main() {
  configure_git_auth
  install_ward
  local work; work="$(clone_target)"
  compose_context
  cd "$work"
  export WARD_REAP_WORK="$work"
  # Arm the reaper before launching the agent; the agent is NOT exec'd, else exec
  # would replace this shell and skip the trap, defeating the backstop.
  trap reap EXIT
  log "ready: $WARD_TARGET_OWNER/$WARD_TARGET_NAME on $(git branch --show-current) [mode=$WARD_MODE]"
  if ! command -v "$WARD_AGENT" >/dev/null 2>&1; then
    log "agent '$WARD_AGENT' is not in this image yet (codex/qwen install is a follow-up); dropping to a shell (reaper runs on exit)"
    bash || true
    return
  fi
  "$WARD_AGENT" "$@" || log "agent exited non-zero ($?); reaping anyway"
}

main "$@"
