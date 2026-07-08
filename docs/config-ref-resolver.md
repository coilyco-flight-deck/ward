---
doc_goal: Pin the WARD_CONFIG_REF git-ref grammar and its TTL-cached syncGitRef resolver - the parse order, cache layout, refresh/offline semantics, and auth.
---
# The `WARD_CONFIG_REF` git grammar and its TTL-cached resolver

Since [ward#654](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/654) (epic [ward#650](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/650)), the git form of `WARD_CONFIG_REF` resolves live
into the local bundle dir the [config-source seam](config-source.md) compiles from.

## Grammar

`WARD_CONFIG_REF = <host>/<owner>/<repo>[@<ref>]//<subpath>`

Example: `forgejo.coilysiren.me/coilyco-flight-deck/agentic-os@main//.ward`

The form parses self-describingly, no forge assumptions
(`cmd/ward/configref.go`): split once on the first `//` (it cannot appear in
host/owner/repo) - left is the repo-spec, right the bundle subpath inside the
checkout (empty means the repo root). Split the repo-spec once on `@` for the
ref; no `@` tracks the remote default branch. The clone URL is
`https://<host>/<owner>/<repo>.git` and the ref is passed to git untouched -
branch, tag, or sha, ward does not classify it. A malformed ref (or a subpath
escaping the checkout) fails loud, never a silent baked fallback.

## The resolver

Resolution rides `syncGitRef` (`cmd/ward/gitsync.go`), the same
mirror-ensure-and-freshen core the container substrate warmer uses:

- **Bare mirror + working checkout** per ref under the config-bundle cache:
  `~/.cache/ward/config-bundle/<hash-of-ref>/` on a host, the shared
  `ward-gitcache` volume in a container (`WARD_CONTAINER=1`; the bootstrap
  pre-creates the dir agent-writable, and an older root-owned volume degrades
  to the home cache rather than bricking the ref).
- **TTL gate** keyed on the mirror's `FETCH_HEAD` mtime: a burst of ward
  invocations inside the window does zero network I/O and touches nothing.
  Default 600s (the substrate warmer's), overridable via `WARD_CONFIG_TTL`
  (seconds; `0` refreshes every invocation).
- **flock serialization** so concurrent ward processes cannot corrupt the
  shared mirror or checkout.
- **Cache-fallback** - a failed refresh logs and serves the cached mirror
  (never brick offline); only a ref that has never synced fails.
- **Auth** rides the pre-configured Forgejo extraheader ([git-verbs.md](git-verbs.md),
  [ward#507](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/507)), resolved lazily only when the sync actually goes to the
  network.

## See also

- [config-source.md](config-source.md) - the seam this resolver feeds: bundle
  layout, selection contract, per-site degrade behavior.
