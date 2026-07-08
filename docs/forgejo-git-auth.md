---
doc_goal: Explain how a container run pushes as the bot rather than the human, and why that attribution is load-bearing for the audit trail, by tracing the group-readable credential file, the store-helper clobber that silently drops it to the env fallback, and how the embedded-entrypoint fix ships on a ward release.
---
# Forgejo git auth

Every container run must land on `main` **as the bot, not as Kai**. That
attribution is load-bearing, not cosmetic: a run's commits are one row in the
durable audit trail, and a merge miscredited to a human breaks the
reconstructable history that lets a reader tell an agent-driven landing from a
hand-driven one. Keeping the push on the bot credential is protecting that
boundary. This doc traces the one perms bug that silently drops it to the human
fallback, and the release-borne fix.

The container pushes over git-over-HTTPS with the bot `FORGEJO_TOKEN`, written
to `/etc/ward-git-credentials` and read by a tiny wrapper helper. The wrapper
answers `get` from the shared file and treats `store` / `erase` as no-op
success, so the dropped agent never tries to lock `/etc/ward-git-credentials`.
Setup runs as root, then the agent drops to non-root via **`setpriv`** (the
entrypoint sheds root and re-execs the agent under the unprivileged **agent
gid**, the group id the dropped agent runs under). So the credential file must
be group-readable by that agent gid (`0640 root:<agent-gid>`) for the push to
use the bot credential.

`ensure_git_cred_readable` still re-asserts the perms right before the
`setpriv` drop, after every root clone, and fails loud if the agent still
cannot read it.

**How this fix propagates ([ward#301](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/301)).** `entrypoint.sh` is **embedded in the
ward binary** (`//go:embed`) and bind-mounted at `/opt/ward`. `docker run
--entrypoint /opt/ward/entrypoint.sh` makes it **override whatever the dev-base
image ships**, which bakes no entrypoint (see `container_compute.go`). So an
`entrypoint.sh` change propagates on a **ward release**, **not** by republishing
the dev-base image. It shipped in `v0.137.0`.

## See also

- [docs/container.md](container.md) - the container internals, the `setpriv`
  privilege-drop, and the agent gid this auth path runs under.
