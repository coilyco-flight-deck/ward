#!/usr/bin/env bash
# ward container entrypoint. Bind-mounted into the aos dev-base image at
# /opt/ward/entrypoint.sh by `ward container up` (it is embedded in the ward
# binary, not baked into the image). Responsibilities, in order: install ward,
# configure forgejo git auth, cached-fresh-clone the target repo, install the
# repo's pre-commit hooks so agent commits hit the same gate a human's do,
# compose the per-mode operating context, then exec the agent. See ward
# docs/container.md.
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
# qwen rides opencode against a local ollama model; tag + endpoint are overridable.
# See docs/agent.md (qwen).
WARD_QWEN_MODEL="${WARD_QWEN_MODEL:-qwen2.5-coder:latest}"
WARD_OLLAMA_URL="${WARD_OLLAMA_URL:-http://localhost:11434/v1}"
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

# Stamp container start (UTC RFC3339); the reaper inherits this via the trap and
# reports the baked Forgejo PAT's age on a salvage issue (ward#103).
export WARD_CONTAINER_UP="${WARD_CONTAINER_UP:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

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

# --- install opencode (qwen mode): best-effort, never fatal -------------------
# opencode is absent from the image; self-install it onto PATH. docs/agent.md (qwen).
install_opencode() {
  [ "$WARD_MODE" = qwen ] || return 0
  if command -v opencode >/dev/null 2>&1; then
    log "opencode already present in image; skipping install"
    return 0
  fi
  log "installing opencode (qwen-backed harness; not baked into the dev-base image yet)"
  if curl -fsSL https://opencode.ai/install | bash >&2; then
    if [ -x "$HOME/.opencode/bin/opencode" ]; then
      install -m 0755 "$HOME/.opencode/bin/opencode" /usr/local/bin/opencode
    fi
  fi
  command -v opencode >/dev/null 2>&1 \
    || log "opencode install failed; qwen mode will drop to a shell (use --image with opencode baked in, or fix network)"
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

# --- pre-commit parity (ward#133): register hooks a fresh clone lacks --------
# Else agent commits bypass the repo's pre-commit gate. See docs/container-precommit.md.
install_precommit_hooks() {
  local work="$1"
  # No config, or no pre-commit in the image: log and move on, never abort startup.
  [ -f "$work/.pre-commit-config.yaml" ] || { log "no .pre-commit-config.yaml in $work; skipping pre-commit install"; return 0; }
  if ! command -v pre-commit >/dev/null 2>&1; then
    log "pre-commit not on PATH; agent commits will NOT run the repo hook suite (ward#133)"
    return 0
  fi
  # The reaper commits --no-verify by design, so this only re-gates the agent's
  # own commits. Hook environments install lazily on first commit (cheap+offline).
  if ( cd "$work" \
        && pre-commit install >&2 \
        && pre-commit install --hook-type commit-msg >&2 ); then
    log "installed pre-commit hooks in $work (ward#133)"
  else
    log "pre-commit install failed in $work; agent commits may bypass the hook suite (ward#133)"
  fi
}

# --- agent-only commit suite (ward#139): headless/task runs only -------------
# Enable agentic-os closes-issue + conventional-commit. See docs/agent-precommit.md.
install_agent_precommit_hooks() {
  local work="$1"
  [ "${WARD_HEADLESS:-0}" = 1 ] || { log "not headless; skipping agent-only commit suite (ward#139)"; return 0; }
  [ -f "$work/.pre-commit-config.yaml" ] || { log "no .pre-commit-config.yaml; skipping agent commit suite (ward#139)"; return 0; }
  command -v pre-commit >/dev/null 2>&1 || { log "pre-commit not on PATH; skipping agent commit suite (ward#139)"; return 0; }
  # Generate an agent-only config pinning the repo's agentic-os rev, then bind it
  # as the commit-msg hook (a repo-relative path so it resolves at commit time).
  local cfg=".git/ward-agent-precommit.yaml"
  if ! ward container agent-precommit-config --config "$work/.pre-commit-config.yaml" > "$work/$cfg" 2>/dev/null; then
    rm -f "$work/$cfg"
    log "no agentic-os hooks to enable; skipping agent commit suite (ward#139)"
    return 0
  fi
  if ( cd "$work" && pre-commit install --hook-type commit-msg --config "$cfg" >&2 ); then
    log "installed agent-only commit-msg suite via $cfg (ward#139)"
  else
    log "agent commit suite install failed (ward#139)"
  fi
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
  # goose ignores ~/.claude/CLAUDE.md; mirror composed doctrine into its hints
  # file so `goose run` carries the seed prompt's context. See docs/agent.md (goose).
  if [ "$WARD_MODE" = goose ]; then
    local ghints="$AGENT_HOME/.config/goose/.goosehints"
    mkdir -p "$(dirname "$ghints")"
    cp "$out" "$ghints"
    log "mirrored composed context into $ghints (goose hints)"
  fi
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

# --- codex credentials (ward#178): host-resolved auth.json, ride --env-file ---
# Host passes ~/.codex/auth.json base64'd; we decode it to the file codex reads.
write_codex_creds() {
  [ "$WARD_MODE" = codex ] || return 0
  [ -n "${WARD_CODEX_AUTH_B64:-}" ] || { log "no codex credentials injected; codex will be unauthenticated (run 'codex login' on the host to seed ~/.codex/auth.json)"; return 0; }
  local dir="$AGENT_HOME/.codex"
  mkdir -p "$dir"
  printf '%s' "$WARD_CODEX_AUTH_B64" | base64 -d > "$dir/auth.json"
  chmod 600 "$dir/auth.json"
  log "wrote codex credentials to $dir/auth.json"
}

# --- codex config (ward#178): approvals-off / sandbox-open posture -----------
# The container is the isolation boundary, so codex needs neither. docs/agent.md.
compose_codex_config() {
  [ "$WARD_MODE" = codex ] || return 0
  local dir="$AGENT_HOME/.codex"
  mkdir -p "$dir"
  cat > "$dir/config.toml" <<'EOF'
# Written by the ward container entrypoint (ward#178): container is the boundary.
approval_policy = "never"
sandbox_mode = "danger-full-access"
EOF
  log "wrote codex config (approvals off, sandbox open) to $dir/config.toml"
}

# --- opencode config (qwen mode): point opencode at a local ollama qwen model -
# Register a local ollama provider + pin the default model. docs/agent.md (qwen).
compose_opencode_config() {
  [ "$WARD_MODE" = qwen ] || return 0
  local dir="$AGENT_HOME/.config/opencode"
  mkdir -p "$dir"
  cat > "$dir/opencode.json" <<EOF
{
  "\$schema": "https://opencode.ai/config.json",
  "model": "ollama/$WARD_QWEN_MODEL",
  "provider": {
    "ollama": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "Ollama (local)",
      "options": { "baseURL": "$WARD_OLLAMA_URL" },
      "models": { "$WARD_QWEN_MODEL": {} }
    }
  }
}
EOF
  log "wrote qwen-backed opencode config (model ollama/$WARD_QWEN_MODEL via $WARD_OLLAMA_URL) to $dir/opencode.json"
}

# --- goose config (ward#186): bind a model provider so goose can run -----------
# Seed ~/.config/goose/config.yaml with provider + model (tower Ollama). docs/agent.md.
compose_goose_config() {
  [ "$WARD_MODE" = goose ] || return 0
  local dir="$AGENT_HOME/.config/goose"
  mkdir -p "$dir"
  local provider="${WARD_GOOSE_PROVIDER:-ollama}"
  local model="${WARD_GOOSE_MODEL:-qwen2.5}"
  local host=""
  [ -n "${WARD_GOOSE_OLLAMA_HOST_B64:-}" ] && host="$(printf '%s' "$WARD_GOOSE_OLLAMA_HOST_B64" | base64 -d)"
  {
    echo "# Written by the ward container entrypoint (ward#186): bind goose's provider."
    echo "GOOSE_PROVIDER: $provider"
    echo "GOOSE_MODEL: $model"
    [ -n "$host" ] && echo "OLLAMA_HOST: $host"
  } > "$dir/config.yaml"
  if [ "$provider" = ollama ] && [ -z "$host" ]; then
    log "wrote goose config (provider=$provider model=$model) to $dir/config.yaml; no tower Ollama host resolved, goose will use its built-in default"
  else
    log "wrote goose config (provider=$provider model=$model) to $dir/config.yaml"
  fi
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
  install_opencode
  local work; work="$(clone_target)"
  install_precommit_hooks "$work"
  install_agent_precommit_hooks "$work"
  warm_substrate
  compose_context
  compose_permissions
  write_claude_creds
  write_codex_creds
  compose_codex_config
  compose_opencode_config
  compose_goose_config
  cd "$work"
  export WARD_REAP_WORK="$work"
  # Arm the reaper before launching the agent; the agent is NOT exec'd, else exec
  # would replace this shell and skip the trap, defeating the backstop.
  trap reap EXIT
  log "ready: $WARD_TARGET_OWNER/$WARD_TARGET_NAME on $(git branch --show-current) [mode=$WARD_MODE]"
  if ! command -v "$WARD_AGENT" >/dev/null 2>&1; then
    log "agent '$WARD_AGENT' is not in this image yet (codex/qwen/goose install is a follow-up); dropping to a shell (reaper runs on exit)"
    bash || true
    return
  fi
  # Build per-mode argv from seed "$@" (claude positional, goose/codex/qwen dialects);
  # headless+ask share the one-shot argv, only claude diverges (ask=plain -p).
  local stream=0
  local -a agent_argv
  local oneshot=0
  { [ "${WARD_HEADLESS:-0}" = 1 ] || [ "${WARD_ASK:-0}" = 1 ]; } && oneshot=1
  case "$WARD_MODE" in
  goose)
    if [ "$oneshot" = 1 ]; then
      agent_argv=(goose run -t "$@")
      log "one-shot: goose run -t <prompt> (goose prints to this log)"
    else
      agent_argv=(goose session)
      [ "$#" -gt 0 ] && log "interactive goose session: seed prompt is not auto-delivered (paste the issue)"
    fi
    ;;
  codex)
    # codex speaks the exec dialect: `codex exec <prompt>` one-shot (prints its own
    # progress, stream stays 0), seeded `codex <seed>` TUI interactive. docs/agent.md.
    if [ "$oneshot" = 1 ]; then
      agent_argv=(codex exec "$@")
      log "one-shot: codex exec <prompt> (codex prints to this log)"
    else
      agent_argv=(codex "$@")
    fi
    ;;
  qwen)
    # opencode `run <prompt>` one-shot (prints its own progress, stream stays 0),
    # seedless TUI interactive; provider/model come from opencode.json, not argv.
    if [ "$oneshot" = 1 ]; then
      agent_argv=(opencode run "$@")
      log "one-shot: opencode run <prompt> (opencode prints to this log)"
    else
      agent_argv=(opencode)
      [ "$#" -gt 0 ] && log "interactive opencode TUI: seed prompt is not auto-delivered (paste the issue)"
    fi
    ;;
  *)
    agent_argv=("$WARD_AGENT")
    if [ "${WARD_ASK:-0}" = 1 ]; then
      # ask: plain `claude -p <question>` so the answer streams clean to the
      # attached terminal (no stream-json progress wrapper). See docs/agent-ask.md.
      agent_argv+=(-p)
      log "ask: $WARD_AGENT -p <question> (one-shot answer to this terminal)"
    elif [ "${WARD_HEADLESS:-0}" = 1 ]; then
      agent_argv+=(-p --verbose --output-format stream-json)
      stream=1
      log "headless: streaming $WARD_AGENT progress to this log"
    fi
    agent_argv+=("$@")
    ;;
  esac
  # Drop to the non-root agent user (claude refuses bypass-perms as root, ward#127);
  # setup ran as root. Keep ANTHROPIC_API_KEY from shadowing the OAuth creds.
  chown -R "$AGENT_UID:$AGENT_GID" "$work" "$AGENT_HOME/.claude" "$AGENT_HOME/.config" "$AGENT_HOME/.codex" 2>/dev/null || true
  unset ANTHROPIC_API_KEY ANTHROPIC_AUTH_TOKEN 2>/dev/null || true
  log "launching $WARD_AGENT as uid $AGENT_UID"
  local launch=(setpriv --reuid="$AGENT_UID" --regid="$AGENT_GID" --init-groups
                env HOME="$AGENT_HOME" "${agent_argv[@]}")
  if [ "$stream" = 1 ]; then
    "${launch[@]}" | stream_progress || log "agent exited non-zero ($?); reaping anyway"
  else
    "${launch[@]}" || log "agent exited non-zero ($?); reaping anyway"
  fi
}

main "$@"
