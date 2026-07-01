#!/usr/bin/env bash
# ward container entrypoint. Bind-mounted into the aos dev-base image at
# /opt/ward/entrypoint.sh by `ward agent` at container bring-up (it is embedded
# in the ward binary, not baked into the image). Responsibilities, in order: install ward,
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
WARD_QWEN_MODEL="${WARD_QWEN_MODEL:-qwen3-coder:30b}"
WARD_OLLAMA_URL="${WARD_OLLAMA_URL:-http://localhost:11434/v1}"
# --ts-sidecar run: the loopback forwarder's no-proxy tower endpoint (ward#359).
# Plain localhost; the forwarder bridges it to the tower through $WARD_TS_SOCKS5.
WARD_TOWER_OLLAMA_LOCAL="${WARD_TOWER_OLLAMA_LOCAL:-http://localhost:11434}"
# Warded-agent commits attribute to the coilyco-ops bot; the email is the
# load-bearing match Forgejo links on (ward#245, docs/agent-attribution.md).
GIT_USER_NAME="${WARD_GIT_NAME:-coilyco-ops}"
GIT_USER_EMAIL="${WARD_GIT_EMAIL:-coilyco-ops@coilysiren.me}"
# Additional writable repos this run was explicitly granted (--repo, ward#230):
# a space-separated owner/name list, each cloned full under /workspace.
WARD_EXTRA_REPOS="${WARD_EXTRA_REPOS:-}"

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
    # Push as the coilyco-ops bot: FORGEJO_TOKEN is the bot's (ward#245).
    printf 'https://%s:%s@%s\n' coilyco-ops "$FORGEJO_TOKEN" "$forgejo_host" > /etc/ward-git-credentials
    # Readable by root (reaper) and the dropped agent group, not world; git's store
    # helper clobbers this on clone, so it's re-asserted before the drop (ward#288).
    chown "root:$AGENT_GID" /etc/ward-git-credentials
    chmod 640 /etc/ward-git-credentials
  else
    log "no FORGEJO_TOKEN: clone/push will only work for anonymous repos"
  fi
}

# Scope the read-only revoke to push-to-this-clone: drop the git push wiring but
# KEEP FORGEJO_TOKEN for dispatch-only (file/launch, not push). agent-surface.md.
revoke_push_credential() {
  rm -f /etc/ward-git-credentials
  git config --system --unset-all credential.helper 2>/dev/null || true
  log "read-only session: dropped this clone's push wiring; FORGEJO_TOKEN kept for dispatch-only (file/launch, no push; ward#315)"
}

# Let the dropped agent reach the mounted docker socket so `warded #N` can dispatch a
# sibling, no host-inode chmod (ward#315, ward#319). See docs/agent-surface.md.
grant_docker_socket_access() {
  local sock=/var/run/docker.sock
  [ -S "$sock" ] || { log "explore: no docker socket mounted - dispatch unavailable this run (ward#315)"; return 0; }
  local sockgid agent_user grp
  sockgid="$(stat -c %g "$sock" 2>/dev/null)" || sockgid=""
  if [ -z "$sockgid" ]; then
    log "explore: could not read docker socket gid; dispatch may fail (ward#315)"
    return 0
  fi
  if [ "$sockgid" = 0 ]; then
    bridge_docker_socket "$sock"  # root:root: no group to join, bridge it (ward#319)
    return 0
  fi
  agent_user="$(getent passwd "$AGENT_UID" | cut -d: -f1)"
  if [ -z "$agent_user" ]; then
    log "explore: no passwd entry for uid $AGENT_UID; cannot group-grant the socket (ward#315)"
    return 0
  fi
  grp="$(getent group "$sockgid" | cut -d: -f1)"
  if [ -z "$grp" ]; then
    grp=dockerhost
    groupadd -g "$sockgid" "$grp" 2>/dev/null || true
  fi
  if usermod -aG "$sockgid" "$agent_user" 2>/dev/null; then
    log "explore: granted docker socket access to $agent_user via group $grp (gid $sockgid); no socket perms changed (ward#315)"
  else
    log "explore: could not add $agent_user to socket group $grp (gid $sockgid); dispatch may fail (ward#315)"
  fi
}

# --- root credential broker (ward#329); the explore hardening, docs/broker.md --
# Root daemon holds the token + serves write-tier forgejo ops over an agent socket.
WARD_BROKER_SOCK_PATH="${WARD_BROKER_SOCK:-/run/ward/broker.sock}"
# Broker daemon logs go to this file, never the shared TTY a read-only director
# TUI owns (ward#389, docs/broker.md). Under the writable socket dir.
WARD_BROKER_LOG_PATH="${WARD_BROKER_LOG:-/run/ward/broker.log}"

# install_ward_kdl_write fetches the write-tier binary the broker shells (release
# path only; best-effort - a miss just leaves the broker unstarted). docs/broker.md.
install_ward_kdl_write() {
  command -v ward-kdl-write >/dev/null 2>&1 && return 0
  if [ -n "${WARD_FROM_SOURCE:-}" ]; then
    log "broker: ward-kdl-write is not built from the mounted source (RO mount); broker skipped this run (ward#331)"
    return 0
  fi
  local tag asset
  tag="$(resolve_ward_tag)"
  [ -n "$tag" ] && [ "$tag" != "null" ] || { log "broker: no release tag resolved for ward-kdl-write; broker skipped"; return 0; }
  asset="$WARD_FORGEJO_BASE/coilyco-flight-deck/ward/releases/download/$tag/ward-kdl-write-linux-$(arch)"
  log "broker: downloading ward-kdl-write $tag for linux-$(arch)"
  if curl -fsSL -H "Authorization: token ${FORGEJO_TOKEN:-}" -o /usr/local/bin/ward-kdl-write "$asset"; then
    chmod 0755 /usr/local/bin/ward-kdl-write
  else
    rm -f /usr/local/bin/ward-kdl-write
    log "broker: ward-kdl-write download failed ($asset); broker skipped (older release without the tier asset?)"
  fi
}

# start_broker brings the broker up and exports WARD_BROKER_SOCK once the socket
# exists. Best-effort: a miss leaves the agent on the dual-mode token path.
start_broker() {
  [ "${WARD_READONLY:-0}" = 1 ] || return 0
  [ -n "${FORGEJO_TOKEN:-}" ] || { log "broker: no FORGEJO_TOKEN to hold; skipping broker"; return 0; }
  install_ward_kdl_write
  command -v ward-kdl-write >/dev/null 2>&1 || return 0
  mkdir -p "$(dirname "$WARD_BROKER_SOCK_PATH")" "$(dirname "$WARD_BROKER_LOG_PATH")"
  log "broker: starting root credential broker on $WARD_BROKER_SOCK_PATH (socket gid $AGENT_GID); daemon log -> $WARD_BROKER_LOG_PATH"
  # fd 1+2 -> log file, NEVER >&2: the daemon logs every served op and would
  # otherwise corrupt the shared read-only director TUI's input bar (ward#389).
  ward container broker --socket "$WARD_BROKER_SOCK_PATH" --group "$AGENT_GID" >>"$WARD_BROKER_LOG_PATH" 2>&1 &
  local waited
  for waited in $(seq 1 15); do
    [ -S "$WARD_BROKER_SOCK_PATH" ] && break
    sleep 0.2
  done
  : "$waited" # silence "unused": the loop's effect is the bounded wait, not the value
  if [ -S "$WARD_BROKER_SOCK_PATH" ]; then
    export WARD_BROKER_SOCK="$WARD_BROKER_SOCK_PATH"
    log "broker: ready; exported WARD_BROKER_SOCK=$WARD_BROKER_SOCK_PATH for the dropped agent"
  else
    log "broker: socket did not appear at $WARD_BROKER_SOCK_PATH; continuing without the broker"
  fi
}

# Bridge a root:root docker socket to an agent-group-owned socket via root socat, so
# the agent reaches it through DOCKER_HOST with no host-perm change (ward#319).
bridge_docker_socket() {
  local sock="$1" bridge=/tmp/docker-agent.sock
  command -v socat >/dev/null 2>&1 || { log "explore: socat absent from image; dispatch unavailable on a root:root socket (ward#319)"; return 0; }
  rm -f "$bridge"
  socat "UNIX-LISTEN:$bridge,fork,group=$AGENT_GID,mode=0660" "UNIX-CONNECT:$sock" &
  export DOCKER_HOST="unix://$bridge"
  log "explore: bridged root:root docker socket to $bridge for the agent (gid $AGENT_GID; ward#319)"
}

# Re-assert the credential perms git's `store` helper clobbers on the clones;
# fail loud if the agent still can't read it (ward#288, docs/agent-credentials.md).
ensure_git_cred_readable() {
  local f=/etc/ward-git-credentials
  [ -e "$f" ] || return 0
  chown "root:$AGENT_GID" "$f"
  chmod 640 "$f"
  if ! setpriv --reuid="$AGENT_UID" --regid="$AGENT_GID" --init-groups \
        sh -c 'test -r /etc/ward-git-credentials'; then
    die "git credential file $f is unreadable by the agent (uid $AGENT_UID gid $AGENT_GID) after re-perm; push would fall back to the human token and leak attribution (ward#288)"
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
  # warded is the public-face shim: the same binary symlinked, multicall-rewritten
  # to `ward agent <args>` so `warded #98` fronts the dispatcher (ward#247, ward#282).
  ln -sf ward /usr/local/bin/warded
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

# --- read-only push guard: strip origin's push URL (ward#327) and land a per-clone
# pre-push hook that fails fast with a clear message (ward#299). agent-surface.md.
install_readonly_push_guard() {
  local work="$1"
  [ "${WARD_READONLY:-0}" = 1 ] || return 0
  # ward#327: point origin's push URL at a dead no-push:// scheme (fetch stays
  # intact) so a push has no target. Synced with revokeClonePushURL; best-effort.
  if git -C "$work" remote set-url --push origin no-push://read-only-explore 2>/dev/null; then
    log "stripped origin push URL on $work -> no-push://read-only-explore (ward#327)"
  else
    log "could not strip push URL on $work; credential drop + pre-push hook still guard it (ward#327)"
  fi
  local dir="$work/.git/hooks"
  [ -d "$dir" ] || { log "no .git/hooks in $work; skipping read-only push guard (ward#299)"; return 0; }
  cat > "$dir/pre-push" <<'HOOK'
#!/bin/sh
# ward#299 read-only explore push guard (message layer; bypassable). See ward#315.
echo "ward: read-only explore - this clone can't push (ward#293, ward#315)." >&2
echo "Commit/branch locally; to ship, file an issue + dispatch 'warded #N'." >&2
exit 1
HOOK
  chmod 0755 "$dir/pre-push"
  log "installed read-only push guard in $work (ward#299)"
}

# --- additional granted repos (ward#230): clone+operate beyond the target -----
# Clone each --repo grant full under /workspace. See docs/container-multi-repo.md.
clone_extra_repo() {
  local owner="$1" name="$2"
  local mirror="$WARD_GITCACHE/${owner}__${name}.git"
  local url="$WARD_FORGEJO_BASE/$owner/$name.git"
  # Refresh the shared bare mirror under an flock (many containers may share it),
  # mirroring warm_substrate_repo; then drop a fresh writable working copy.
  (
    flock 9
    if [ -d "$mirror" ]; then
      log "extra-repo: refreshing cached mirror $owner/$name"
      git -C "$mirror" remote update --prune >&2 || log "extra-repo: mirror refresh failed $owner/$name (using cached state)"
    else
      log "extra-repo: cloning mirror (first time) $owner/$name"
      git clone --mirror "$url" "$mirror" >&2 \
        || { log "extra-repo: mirror clone failed $owner/$name (skipping)"; rm -rf "$mirror"; exit 0; }
    fi
  ) 9>"$WARD_GITCACHE/.${owner}__${name}.lock" || true
  [ -d "$mirror" ] || return 0
  local dest="/workspace/$name"
  rm -rf "$dest"
  if ! git clone "$mirror" "$dest" >&2; then
    log "extra-repo: working clone failed $owner/$name"
    return 0
  fi
  git -C "$dest" remote set-url origin "$url"
  git -C "$dest" config push.default current
  [ -n "${WARD_BRANCH:-}" ] && git -C "$dest" checkout -B "$WARD_BRANCH" >&2
  install_precommit_hooks "$dest"
  install_readonly_push_guard "$dest"
  log "extra-repo: ready $owner/$name at $dest"
  return 0
}

clone_extra_repos() {
  [ -n "${WARD_EXTRA_REPOS:-}" ] || return 0
  mkdir -p "$WARD_GITCACHE"
  local ref owner name
  for ref in $WARD_EXTRA_REPOS; do
    owner="${ref%%/*}"; name="${ref##*/}"
    [ -n "$owner" ] && [ -n "$name" ] || continue
    # The target is cloned by clone_target; never re-clone it as an extra.
    if [ "$owner" = "$WARD_TARGET_OWNER" ] && [ "$name" = "$WARD_TARGET_NAME" ]; then continue; fi
    clone_extra_repo "$owner" "$name"
  done
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
link_or_copy_context() {
  local link_target="$1" src="$2" dest="$3"
  mkdir -p "$(dirname "$dest")"
  rm -f "$dest"
  ln -s "$link_target" "$dest" 2>/dev/null || cp "$src" "$dest"
}

compose_context() {
  local out="$AGENT_HOME/AGENTS.md"
  mkdir -p "$(dirname "$out")"
  cat /opt/ward/AGENTS.container.md > "$out"
  if [ "$WARD_CONTEXT_LEVEL" -ge 2 ] && [ -d "$WARD_CONTEXT_SRC" ]; then
    for f in CLAUDE.md AGENTS.md; do
      [ -f "$WARD_CONTEXT_SRC/$f" ] && { printf '\n\n---\n\n' >> "$out"; cat "$WARD_CONTEXT_SRC/$f" >> "$out"; }
    done
  elif [ "$WARD_CONTEXT_LEVEL" -eq 1 ] && [ -f "$WARD_CONTEXT_SRC/AGENTS.md" ]; then
    printf '\n\n---\n\n' >> "$out"; cat "$WARD_CONTEXT_SRC/AGENTS.md" >> "$out"
  fi
  # Read-only static entry context (a seedless run has no seed to carry it; ward#293).
  # Kept in sync with readOnlyContextBlock in container_bootstrap.go.
  if [ "${WARD_READONLY:-0}" = 1 ]; then
    cat >> "$out" <<'EOF'


---

## Read-only session (this overrides the autonomy doctrine above)

This is the **director's read-only surface session** (`warded director` surfaced it when the
headless lane drained, or at startup before the first drain). Here "read-only" means one
thing: **this clone cannot push to its own remote**, so nothing leaves this clone. It does
not mean you are sealed off. The natural product of a surface session is commissioned work,
and that still ships.

Capture-and-dispatch is an **obligation, not a "may"**. Every work item you surface -
a bug, a missing test, a follow-up, anything worth doing - you **must**:

- **File an issue** for it (`ward ops forgejo issue create ...`), then
- **Dispatch a sibling headless run** to do the actual fix - `warded <owner/repo>#N`
  spins up its own sealed container with its own credential and lifecycle, does its
  own implement -> commit -> merge -> push there, and never touches this clone.

Do not let a work item die in the conversation. If you named it, capture it and
dispatch it before you move on.

**Capture-and-dispatch and move on without babysitting.** The director heartbeat that
surfaced you is what polls outcomes, reconciles the lane, and does the chatty back-and-forth
- your job in this seat is to read, scope, file, and fire, then **exit to hand control back
to the heartbeat**. You file the issue, fire the headless run, and let it carry itself to
merge - you do not sit on it, poll it, or wait for it to report back.

**Prefer a sibling dispatch over an in-session subagent.** When the work is
delegable - a design proposal, a research dig, an implementation - reach for a sibling
warded run (`warded advisor #N` to design or research, `warded engineer #N` to build)
before an in-session subagent. The sibling lands a durable, attributable artifact on
the canonical surface (the issue thread, a pushed commit) that outlives this session,
and the next run can read it. A subagent's output dies in this conversation's
scrollback. Reserve an in-session subagent for read-only fan-out that only feeds
**your** immediate reasoning and never needs to outlive the session.

**How this is wired** (you do not set any of it up - it is ready):

- A `FORGEJO_TOKEN` (the coilyco-ops bot's) is present, so `ward ops forgejo ...` and
  the dispatcher authenticate out of the box. The token is the bot's full credential,
  so the no-push rule below is a convention you keep, not yet a credential boundary
  (a dispatch-only token is tracked in ward#318).
- A host dispatch broker is reachable over TCP at `$WARD_DISPATCH_BROKER_ADDR`
  (guarded by `$WARD_DISPATCH_BROKER_TOKEN`), so a dispatched `warded #N`, `warded
  engineer #N`, or `warded advisor #N "question"` asks host ward to launch the
  sibling from the native host context. Host ward resolves Claude/Codex/Goose
  credentials there and launches the child; this container does not need a raw
  Docker dispatch surface for that path.

You **must not**:

- Commit and push **this clone**, or merge this clone's tree to `main`.
- Hand-build an authenticated push URL to get this clone's tree onto the remote by
  another route. (A dispatch-only credential is the proper guard here; until it
  lands, this is a convention you keep - ward#318.)

This clone's push wiring has been removed, so a direct `git push` from here fails.
Read the repo, reason about it, answer questions, scratch in the working tree if it
helps you think - then either **file + dispatch** the work or just exit.
EOF
  fi
  log "composed context (level $WARD_CONTEXT_LEVEL$([ "${WARD_READONLY:-0}" = 1 ] && echo ', read-only')) at $out"
  link_or_copy_context "../AGENTS.md" "$out" "$AGENT_HOME/.claude/CLAUDE.md"
  log "linked Claude context load point to $out"
  link_or_copy_context "../AGENTS.md" "$out" "$AGENT_HOME/.codex/AGENTS.md"
  log "linked Codex context load point to $out"
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
# bypassPermissions, no deny list - isolation is the sole wall (docs/container.md).
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
  # Bootstrap-only channel; scrub it so the live OAuth token can't leak on a
  # subprocess `env` dump (ward#357). Mirrors the git-cred scrub above.
  unset WARD_CLAUDE_CREDS_B64
  log "wrote claude credentials to $dir/.credentials.json (scrubbed WARD_CLAUDE_CREDS_B64 from env)"
}

# --- claude onboarding seed (ward#305, ward#313): skip the first-run gates -----
# Theme picker (ward#305) + bypass-mode acceptance & folder trust (ward#313).

# Trust is keyed under the launch cwd ($work=/workspace/<tgt>); without these an
# interactive/headless explore hangs on the unanswered accept-risk/trust dialogs.
seed_claude_onboarding() {
  [ "$WARD_MODE" = claude ] || return 0
  local work="$1"
  local out="$AGENT_HOME/.claude.json"
  mkdir -p "$AGENT_HOME"
  jq -n --arg work "$work" '{
    hasCompletedOnboarding: true,
    theme: "dark",
    bypassPermissionsModeAccepted: true,
    projects: { ($work): { hasTrustDialogAccepted: true, hasCompletedProjectOnboarding: true } }
  }' > "$out"
  log "seeded claude onboarding (skip first-run wizard + bypass/trust gates) at $out"
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
  # Bootstrap-only delivery channel; codex reads the file, not the env. Scrub it so
  # the ChatGPT/API-key blob does not linger in the agent's environment (ward#357).
  unset WARD_CODEX_AUTH_B64
  log "wrote codex credentials to $dir/auth.json (scrubbed WARD_CODEX_AUTH_B64 from env)"
}

# --- codex config (ward#178): approvals-off / sandbox-open + cheapest posture -
# Cheapest codex settings by default (ward#379); WARD_CODEX_* overrides.
compose_codex_config() {
  [ "$WARD_MODE" = codex ] || return 0
  local dir="$AGENT_HOME/.codex"
  mkdir -p "$dir"
  local model="${WARD_CODEX_MODEL:-gpt-5.4-mini}"
  local effort="${WARD_CODEX_REASONING_EFFORT:-low}"
  local verbosity="${WARD_CODEX_VERBOSITY:-low}"
  cat > "$dir/config.toml" <<EOF
# Written by the ward container entrypoint (ward#178): container is the boundary.
approval_policy = "never"
sandbox_mode = "danger-full-access"
# Cheapest codex settings by default (ward#379); override with WARD_CODEX_*.
model = "$model"
model_reasoning_effort = "$effort"
model_verbosity = "$verbosity"
EOF
  log "wrote codex config (approvals off, sandbox open, model $model / effort $effort / verbosity $verbosity) to $dir/config.toml"
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
  local model="${WARD_GOOSE_MODEL:-qwen3-coder:30b}"
  local host=""
  [ -n "${WARD_GOOSE_OLLAMA_HOST_B64:-}" ] && host="$(printf '%s' "$WARD_GOOSE_OLLAMA_HOST_B64" | base64 -d)"
  # The tower host (tailnet endpoint) is the secret in this env; scrub it once
  # decoded - same treatment as the cred blobs (ward#357).
  unset WARD_GOOSE_OLLAMA_HOST_B64
  # --ts-sidecar, no SSM host: route goose at the loopback forwarder (ward#359).
  if [ -z "$host" ] && [ -n "${WARD_TS_SOCKS5:-}" ]; then
    host="localhost:11434"
    log "goose: binding OLLAMA_HOST=$host (the --ts-sidecar loopback forwarder; ward#359)"
  fi
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

# --- tower loopback forwarder (--ts-sidecar, ward#359): userspace SOCKS5->TCP so
# the tower is plain localhost:11434, no --proxy, no cap. docs/agent-ts-sidecar.md.
TOWER_FORWARDER_PID=""

start_tower_forwarder() {
  # Only in a --ts-sidecar run (WARD_TS_SOCKS5 present); a non-sidecar run is
  # unchanged. ward backgrounds the forwarder it already installed.
  [ -n "${WARD_TS_SOCKS5:-}" ] || return 0
  if ! command -v ward >/dev/null 2>&1; then
    log "tower forwarder: ward not on PATH, skipping localhost:11434 forwarder (the --proxy path still works)"
    return 0
  fi
  ward container forward &
  TOWER_FORWARDER_PID=$!
  log "tower forwarder up (pid $TOWER_FORWARDER_PID): dial the tower at \$WARD_TOWER_OLLAMA_LOCAL ($WARD_TOWER_OLLAMA_LOCAL) with no --proxy, e.g. curl \"\$WARD_TOWER_OLLAMA_LOCAL/api/tags\" (ward#359)"
}

stop_tower_forwarder() {
  [ -n "$TOWER_FORWARDER_PID" ] || return 0
  kill "$TOWER_FORWARDER_PID" 2>/dev/null || true
  TOWER_FORWARDER_PID=""
}

# --- reaper: deterministic teardown backstop (docs/container-reap.md) --------
# Static ward code lands/salvages residual work on any agent exit; nothing lost.
reap() {
  trap - EXIT
  [ -n "${WARD_REAP_WORK:-}" ] || return 0
  # A read-only explore session never mutates the remote (ward#293): skip salvage.
  [ "${WARD_READONLY:-0}" = 1 ] && { log "read-only session: nothing to salvage (skipping reap)"; return 0; }
  log "reaping: salvage residual work before teardown"
  ward container reap --work "$WARD_REAP_WORK" \
    || log "reaper returned non-zero; check this log for an UNPRESERVED PATCH block before the container is removed"
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

# --- pre-launch auth smoke test (ward#222; disk-aware diagnostics ward#273) --

# Bounded claude probe as the agent user. A full disk stalls startup like a bad
# credential, so a stall reports disk, not the login (see docs/agent-credentials.md).
SMOKE_DISK_PATHS="/ /workspace"
SMOKE_DISK_FLOOR_KB=524288 # 512MiB, matches smokeTestDiskFloorBytes in the Go port

# disk_report PATH... -> "/ 1.2G free of 50G; /workspace ..." (ward#273)
disk_report() {
  local p avail size out=""
  for p in "$@"; do
    [ -e "$p" ] || continue
    avail="$(df -h -P "$p" 2>/dev/null | awk 'NR==2{print $4}')"
    size="$(df -h -P "$p" 2>/dev/null | awk 'NR==2{print $2}')"
    [ -n "$avail" ] || continue
    out="${out:+$out; }$p $avail free of $size"
  done
  [ -n "$out" ] && printf '%s' "$out" || printf 'disk usage unavailable'
}

# low_disk_warn: warn loudly if any smoke-test path is under the headroom floor.
low_disk_warn() {
  local p avail
  for p in $SMOKE_DISK_PATHS; do
    [ -e "$p" ] || continue
    avail="$(df -P "$p" 2>/dev/null | awk 'NR==2{print $4}')"
    [ -n "$avail" ] || continue
    if [ "$avail" -lt "$SMOKE_DISK_FLOOR_KB" ]; then
      log "auth smoke test: WARNING low disk before probe - $(disk_report $SMOKE_DISK_PATHS); a claude startup hang here is likely disk exhaustion, not credentials (ward#273)"
      return 0
    fi
  done
  return 0 # set -e: a fall-through must not exit on the last falsy [ -lt ] test
}

smoke_test_claude_auth() {
  { [ "$WARD_AGENT" = claude ] && [ "${WARD_HEADLESS:-0}" = 1 ]; } || return 0
  [ "${WARD_SMOKE_TEST_SKIP:-0}" = 1 ] && { log "auth smoke test skipped (WARD_SMOKE_TEST_SKIP=1)"; return 0; }
  command -v claude >/dev/null 2>&1 || return 0
  low_disk_warn
  log "auth smoke test: probing claude before launch (ward#222)"
  local out rc=0 errf err
  errf="$(mktemp 2>/dev/null || echo /tmp/ward-smoke-stderr.$$)"
  out="$(timeout 90 setpriv --reuid="$AGENT_UID" --regid="$AGENT_GID" --init-groups \
          env HOME="$AGENT_HOME" claude -p --output-format json \
          "Reply with the single word: ok" </dev/null 2>"$errf")" || rc=$?
  err="$(cat "$errf" 2>/dev/null)"; rm -f "$errf"
  if [ "$rc" -eq 124 ]; then
    die "auth smoke test: claude -p did not respond within 90s - a startup hang, not necessarily an auth problem (ward#222, ward#273). Likely causes: a full disk (claude cannot write ~/.claude), network, or a slow cold start. Disk: $(disk_report $SMOKE_DISK_PATHS). If disk is low, free space on the Docker host; otherwise refresh the host claude login (re-run 'claude' on the host) and relaunch. WARD_SMOKE_TEST_SKIP=1 bypasses."
  fi
  if [ "$rc" -ne 0 ] || [ -z "$out" ]; then
    if printf '%s\n%s' "$err" "$out" | grep -qiE 'not logged in|401|invalid api key|authentication_error|unauthorized|please run /login'; then
      die "auth smoke test: claude -p rejected the credentials (exit $rc) - they are unusable in-container (ward#222). Refresh the host claude login (re-run 'claude' on the host) and relaunch; WARD_SMOKE_TEST_SKIP=1 bypasses."
    fi
    die "auth smoke test: claude -p produced no usable output (exit $rc) without an auth error - more likely a disk/network/startup problem than credentials (ward#222, ward#273). Disk: $(disk_report $SMOKE_DISK_PATHS). WARD_SMOKE_TEST_SKIP=1 bypasses."
  fi
  log "auth smoke test: claude responded, auth OK"
}

# --- launch ------------------------------------------------------------------
main() {
  configure_git_auth
  install_ward
  # EXPERIMENTAL opt-in (ward#181): hand off to the Go bootstrap once ward installs.
  if [ "${WARD_USE_GO_BOOTSTRAP:-0}" = 1 ] && command -v ward >/dev/null 2>&1; then
    log "delegating to the Go container bootstrap (ward#181, WARD_USE_GO_BOOTSTRAP=1)"
    cd /workspace 2>/dev/null || true
    exec ward container bootstrap "$@"
  fi
  install_opencode
  local work; work="$(clone_target)"
  install_precommit_hooks "$work"
  install_readonly_push_guard "$work"
  clone_extra_repos
  warm_substrate
  compose_context
  compose_permissions
  write_claude_creds
  seed_claude_onboarding "$work"
  write_codex_creds
  compose_codex_config
  compose_opencode_config
  compose_goose_config
  cd "$work"
  export WARD_REAP_WORK="$work"
  # Arm the reaper (+ tower forwarder teardown, ward#359) before launching the agent;
  # the agent is NOT exec'd, else exec would skip the trap, defeating the backstop.
  trap 'stop_tower_forwarder; reap' EXIT
  log "ready: $WARD_TARGET_OWNER/$WARD_TARGET_NAME on $(git branch --show-current) [mode=$WARD_MODE]"
  # --ts-sidecar run: surface the tower route (by MagicDNS name through the proxy)
  # the agent can dial; both vars are plain in the agent's env (ward#337).
  if [ -n "${WARD_TS_SOCKS5:-}" ]; then
    log "tailnet route ready: dial the tower at \$WARD_TOWER_OLLAMA (${WARD_TOWER_OLLAMA:-unset}) through \$WARD_TS_SOCKS5 ($WARD_TS_SOCKS5), e.g. curl --proxy \"\$WARD_TS_SOCKS5\" \"\$WARD_TOWER_OLLAMA/api/tags\" (ward#337)"
  fi
  # Start the no-proxy loopback forwarder (ward#359); no-op unless --ts-sidecar.
  start_tower_forwarder
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
      # attached terminal (no stream-json progress wrapper). See docs/agent-advisor.md.
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
  chown -R "$AGENT_UID:$AGENT_GID" "$work" "$AGENT_HOME/AGENTS.md" "$AGENT_HOME/.claude" "$AGENT_HOME/.claude.json" "$AGENT_HOME/.config" "$AGENT_HOME/.codex" 2>/dev/null || true
  # Hand each granted extra-repo tree to the agent user too (ward#230); cloned as root.
  for ref in ${WARD_EXTRA_REPOS:-}; do chown -R "$AGENT_UID:$AGENT_GID" "/workspace/${ref##*/}" 2>/dev/null || true; done
  if [ "${WARD_READONLY:-0}" = 1 ]; then
    revoke_push_credential    # explore: drop this clone's push wiring, keep the dispatch token (ward#315)
    start_broker              # explore: root credential broker holds the token; agent reaches the forge via the socket (ward#329)
  else
    ensure_git_cred_readable # re-assert creds the clones clobbered (ward#288)
  fi
  unset ANTHROPIC_API_KEY ANTHROPIC_AUTH_TOKEN 2>/dev/null || true
  # Fail loud before launch if claude can't authenticate (ward#222): a clear
  # abort beats a silent multi-minute hang. Runs as the agent user, post-chown.
  smoke_test_claude_auth
  # Mark that we reached the real agent launch (post-smoke-test); the reaper reads
  # this on a clean teardown to release a pre-launch-death hold (ward#264, docs).
  export WARD_AGENT_LAUNCHED=1
  log "launching $WARD_AGENT as uid $AGENT_UID"
  local launch=(setpriv --reuid="$AGENT_UID" --regid="$AGENT_GID" --init-groups
                env HOME="$AGENT_HOME" "${agent_argv[@]}")
  # One-shot modes (headless/ask) take no interactive input; pin stdin to /dev/null
  # so a stuck agent gets EOF and exits instead of blocking forever (ward#222).
  if [ "$stream" = 1 ]; then
    "${launch[@]}" </dev/null | stream_progress || log "agent exited non-zero ($?); reaping anyway"
  elif [ "$oneshot" = 1 ]; then
    "${launch[@]}" </dev/null || log "agent exited non-zero ($?); reaping anyway"
  else
    "${launch[@]}" || log "agent exited non-zero ($?); reaping anyway"
  fi
}

main "$@"
