# Forgejo git auth

The container pushes over git-over-HTTPS with the bot `FORGEJO_TOKEN`, written
to `/etc/ward-git-credentials` and wired as git's `store` helper. Setup is
root, then the agent drops to non-root, so the file must be group-readable by
the agent gid (`0640 root:<agent-gid>`) for the push to use the bot credential.

**The clobber (ward#288).** git's `store` helper rewrites it to `0600 root:root`
on each successful auth, so the root-phase clones strip the group-read perms.
An unreadable file then sends the push down git's env fallback
(`FORGEJO_TOKEN`) - attributing the merge to `coilysiren`, not the bot.
`ensure_git_cred_readable` re-asserts the perms right before the `setpriv`
drop, after every root clone, and fails loud if the agent still cannot read it.

**How this fix propagates (ward#301).** `entrypoint.sh` is **embedded in the
ward binary** (`//go:embed`) and bind-mounted at `/opt/ward`; `docker run
--entrypoint /opt/ward/entrypoint.sh` makes it **override whatever the dev-base
image ships**, which bakes no entrypoint (see `container_compute.go`). So an
`entrypoint.sh` change propagates on a **ward release**, **not** by republishing
the dev-base image. It shipped in `v0.137.0`.
