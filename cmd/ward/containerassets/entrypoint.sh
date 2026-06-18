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

# The agent process drops to this non-root user: claude refuses
# --dangerously-skip-permissions as root (ward#127). Setup stays root.
AGENT_UID="${WARD_AGENT_UID:-1000}"
AGENT_GID="${WARD_AGENT_GID:-1000}"
AGENT_HOME="${WARD_AGENT_HOME:-/home/ubuntu}"

# The container is the isolation boundary; its restricted namespace denies ward's
# jail (cli-guard#153), breaking the reaper. Opt out. See cli-guard docs/sandbox.md.
export CLIGUARD_NO_SANDBOX=1

forgejo_host="$(printf '%s' "$WARD_FORGEJO_BASE" | sed -E 's#^https?://##; s#/.*$##')"

# --- forgejo git auth (token rides --env-file, never argv) -------------------
# Written --system so the root reaper and the dropped non-root agent both read it.
configure_git_auth() {
  git config --system user.name "$GIT_USER_NAME"
  git config --system user.email "$GIT_USER_EMAIL"
  git config --system init.defaultBranch main
  # The clone is chowned to the agent user, so root (reaper) and the agent both
  # operate a tree they may not own; bless it to avoid git's dubious-ownership halt.
  git config --system --add safe.directory '*'
  if [ -n "${FORGEJO_TOKEN:-}" ]; then
    git config --system credential.helper 'store --file=/etc/ward-git-credentials'
    printf 'https://%s:%s@%s\n' coilysiren "$FORGEJO_TOKEN" "$forgejo_host" > /etc/ward-git-credentials
    # Readable by root (reaper) and the dropped agent group, not world. Without
    # this the non-root agent can't use the helper and must fall back to the env.
    chown "root:$AGENT_GID" /etc/ward-git-credentials
    chmod 640 /etc/ward-git-credentials
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

# --- warm the substrate reference repos (best-effort; see docs/container.md) --
# Mirror+TTL-refresh each manifest repo, then drop a working copy under DEST.
WARD_SUBSTRATE_SEED="${WARD_SUBSTRATE_SEED:-/opt/substrate-seed}"
WARD_SUBSTRATE_DEST="${WARD_SUBSTRATE_DEST:-/substrate}"
WARD_SUBSTRATE_MANIFEST="${WARD_SUBSTRATE_MANIFEST:-/opt/ward/preclone-repos.txt}"
WARD_SUBSTRATE_TTL="${WARD_SUBSTRATE_TTL:-600}"

# substrate_mirror_stale: true when the mirror's last fetch is older than the
# TTL. A missing FETCH_HEAD (just cloned/hydrated) counts as fresh, not stale.
substrate_mirror_stale() {
  local head="$1/FETCH_HEAD"
  [ -f "$head" ] || return 1
  local age=$(( $(date +%s) - $(stat -c %Y "$head" 2>/dev/null || echo 0) ))
  [ "$age" -ge "$WARD_SUBSTRATE_TTL" ]
}

# warm_substrate_repo: ensure one repo's bare mirror exists+fresh (under an
# flock so concurrent inits serialise), drop a working copy, always return 0.
warm_substrate_repo() {
  local owner="$1" name="$2" tier="$3"
  local mirror="$WARD_GITCACHE/${owner}__${name}.git"
  local seed="$WARD_SUBSTRATE_SEED/${owner}__${name}.git"
  local url="$WARD_FORGEJO_BASE/$owner/$name.git"
  (
    flock 9
    if [ ! -d "$mirror" ]; then
      if [ "$tier" = image ] && [ -d "$seed" ]; then
        log "substrate: hydrate $owner/$name from baked seed"
        cp -a "$seed" "$mirror"
      else
        log "substrate: clone mirror (first time) $owner/$name"
        git clone --mirror "$url" "$mirror" >&2 \
          || { log "substrate: mirror clone failed $owner/$name (skipping)"; rm -rf "$mirror"; exit 0; }
      fi
    fi
    if substrate_mirror_stale "$mirror"; then
      log "substrate: refresh $owner/$name (TTL ${WARD_SUBSTRATE_TTL}s elapsed)"
      git -C "$mirror" remote update --prune >&2 || log "substrate: refresh failed $owner/$name (using cached state)"
    fi
  ) 9>"$WARD_GITCACHE/.${owner}__${name}.lock" || true
  if [ -d "$mirror" ]; then
    local work="$WARD_SUBSTRATE_DEST/$name"
    rm -rf "$work"
    git clone --quiet "$mirror" "$work" >&2 \
      && git -C "$work" remote set-url origin "$url" \
      || log "substrate: working clone failed $owner/$name"
  fi
  return 0
}

warm_substrate() {
  [ "${WARD_SUBSTRATE_SKIP:-0}" = 1 ] && { log "substrate warming skipped (WARD_SUBSTRATE_SKIP=1)"; return 0; }
  [ -f "$WARD_SUBSTRATE_MANIFEST" ] || { log "substrate: no manifest at $WARD_SUBSTRATE_MANIFEST (skipping)"; return 0; }
  mkdir -p "$WARD_GITCACHE" "$WARD_SUBSTRATE_DEST"
  local ref tier owner name
  while read -r ref tier _; do
    case "$ref" in ''|\#*) continue ;; esac
    owner="${ref%%/*}"; name="${ref##*/}"
    [ -n "$owner" ] && [ -n "$name" ] || continue
    # The target repo is cloned into /workspace by clone_target; skip it here.
    if [ "$owner" = "$WARD_TARGET_OWNER" ] && [ "$name" = "$WARD_TARGET_NAME" ]; then
      continue
    fi
    warm_substrate_repo "$owner" "$name" "${tier:-cache}"
  done < "$WARD_SUBSTRATE_MANIFEST"
  log "substrate ready under $WARD_SUBSTRATE_DEST"
}

# --- compose per-mode operating context (the least-context ladder) -----------
# Levels: 2=doctrine+host context, 1=doctrine+host AGENTS.md, 0=doctrine only.
compose_context() {
  local out="$AGENT_HOME/.claude/CLAUDE.md"
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

# --- container permission policy (the container is the permission manager) ----
# bypassPermissions + a minimal force-push/history-rewrite deny (docs/container.md).
compose_permissions() {
  local out="$AGENT_HOME/.claude/settings.json"
  mkdir -p "$(dirname "$out")"
  cp /opt/ward/settings.container.json "$out"
  log "wrote container permission policy to $out"
}

# --- claude credentials (Max OAuth; host-resolved, ride --env-file) ----------
# Host passes the credential base64'd; we decode it to the file claude reads.
write_claude_creds() {
  [ -n "${WARD_CLAUDE_CREDS_B64:-}" ] || { log "no claude credentials injected; claude will be unauthenticated"; return 0; }
  local dir="$AGENT_HOME/.claude"
  mkdir -p "$dir"
  printf '%s' "$WARD_CLAUDE_CREDS_B64" | base64 -d > "$dir/.credentials.json"
  chmod 600 "$dir/.credentials.json"
  log "wrote claude credentials to $dir/.credentials.json"
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

# --- headless progress (claude stream-json -> concise log lines) -------------
# One line/event: text, "● <Tool> <arg>", errors, result. fromjson? skips junk.
stream_progress() {
  jq -Rr --unbuffered 'fromjson? |
    if .type == "assistant" then
      ( .message.content[]? |
        if .type == "text" then
          ( (.text // "") | gsub("\n";" ") ) as $t
          | if ($t | length) > 0 then "  " + $t[0:140] else empty end
        elif .type == "tool_use" then
          "● " + .name + " "
          + ( (.input.file_path // .input.command // .input.path // .input.pattern // .input.url // "")
              | tostring | gsub("\n";" ") | .[0:120] )
        else empty end )
    elif .type == "user" then
      ( .message.content[]? | select(.type == "tool_result" and .is_error == true) | "  ✗ (tool error)" )
    elif .type == "result" then
      ( "✓ result: " + (.subtype // "?")
        + " (" + ((.num_turns // 0) | tostring) + " turns, "
        + (((.duration_ms // 0) / 1000) | floor | tostring) + "s)" ),
      ( (.result // "") | select(length > 0) )
    else empty end'
}

# --- launch ------------------------------------------------------------------
main() {
  configure_git_auth
  install_ward
  local work; work="$(clone_target)"
  warm_substrate
  compose_context
  compose_permissions
  write_claude_creds
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
  # Headless (`ward agent <name> headless`): -p runs to completion; stream-json +
  # stream_progress surface live progress in the log. claude-only (in-image).
  local agent_argv=("$WARD_AGENT") headless=0
  if [ "${WARD_HEADLESS:-0}" = 1 ]; then
    agent_argv+=(-p --verbose --output-format stream-json)
    headless=1
    log "headless: streaming $WARD_AGENT progress to this log"
  fi
  # Drop to the non-root agent user (claude refuses bypass-perms as root, ward#127);
  # setup ran as root. Keep ANTHROPIC_API_KEY from shadowing the OAuth creds.
  chown -R "$AGENT_UID:$AGENT_GID" "$work" "$AGENT_HOME/.claude" 2>/dev/null || true
  unset ANTHROPIC_API_KEY ANTHROPIC_AUTH_TOKEN 2>/dev/null || true
  log "launching $WARD_AGENT as uid $AGENT_UID"
  local launch=(setpriv --reuid="$AGENT_UID" --regid="$AGENT_GID" --init-groups
                env HOME="$AGENT_HOME" "${agent_argv[@]}" "$@")
  if [ "$headless" = 1 ]; then
    "${launch[@]}" | stream_progress || log "agent exited non-zero ($?); reaping anyway"
  else
    "${launch[@]}" || log "agent exited non-zero ($?); reaping anyway"
  fi
}

main "$@"
