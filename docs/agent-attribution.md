# Agent attribution on Forgejo write bodies

Per [ward#155](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/155),
every Forgejo write **ward itself emits** that carries a content body is signed
with the driving agent's identity before it is sent, so a human or another agent
reading the issue/comment/commit can tell who wrote it. The reaper's residual
commit carries a matching `Co-Authored-By` trailer.

## Identity

The identity is derived from the agent mode (`cmd/ward/agent_signature.go`):

| mode   | attribution        |
| ------ | ------------------ |
| claude | `Claude (she/her)` |
| codex  | `Codex`            |
| qwen   | `Qwen`             |
| goose  | `Goose`            |

Only claude carries pronouns today; the rest sign by name. An unrecognized mode
resolves whole to the claude identity, mirroring the claude default elsewhere.

## How it is applied

`signBody` appends an attribution footer to a markdown body, and `commitTrailer`
renders the git `Co-Authored-By` line. Signing happens once, at the
`forgejoClient.createIssue` / `commentIssue` choke points, so every ward-emitted
write body is attributed without each call site remembering to. It is idempotent
- a hidden `<!-- ward-agent-signature -->` marker guards against a double sign.

The mode is read from `WARD_AGENT`, then `WARD_MODE` (the in-container case), or
pinned explicitly via `forgejoClient.withMode` by host-side callers - the
reservation comment, the preflight NO-GO comment, and `ward agent task` issue
filing - that already know the mode rather than inheriting it from the env.

## Git commit author + push identity (the coilyco-ops bot)

Distinct from the body footer above: the **git author/committer** on
warded-agent commits, the **tap-bump author** in `.forgejo/workflows/release.yml`,
and the **git-over-HTTPS push user** all attribute to the `coilyco-ops` bot
([ward#245](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/245),
folding in agentic-os#252 and infra#384 step 2).

Per agentic-os#244 an aos bot uses its agent name as both git name and
git-email prefix. The **email is load-bearing**: Forgejo links a commit to an
account by matching the commit email against an address registered on that
account, so the bot's registered email (`coilyco-ops@coilysiren.me`, set by
infrastructure's `provision-coilyco-ops-bot.sh`) turns a plain author string
into an account-linked, avatar-bearing attribution. The display **name** may
stay descriptive (e.g. `forgejo-tap-writer` on tap bumps).

Three knobs carry this, all defaulting to the bot and overridable by env:

- **Warded-agent author** - `WARD_GIT_NAME` / `WARD_GIT_EMAIL`, defaulting to
  the bot in both `entrypoint.sh` and Go `container_bootstrap.go`. Replaces the
  old `ward-container <coilysiren@gmail.com>`.
- **Push user** - the git-over-HTTPS userinfo in the credential helper line is
  `coilyco-ops`; the `FORGEJO_TOKEN` it pairs with is the bot's.
- **Tap bump** - `release.yml`'s `bump-tap-formula` sets `user.email` to the bot
  (keeping the `forgejo-tap-writer` name); push auth rides
  `secrets.TAP_WRITE_TOKEN` (ward#243), untouched here.

The bot account must have the chosen email registered for the link to resolve;
that registration is an infrastructure step, not a ward code concern.

## Out of scope: the ward-kdl specverb path

`ward-kdl ops forgejo` (the spec-driven REST path) assembles its bodies in
cli-guard's `specverb` engine, not in this repo, so its `create issue` /
`comment issue` bodies are **not** signed here. Signing them universally "via kdl
spec" is a body-footer directive on the `wrap` that belongs upstream in
cli-guard; see [docs/ops-forgejo.md](ops-forgejo.md).

## See also

- [docs/container-reap.md](container-reap.md) - the reaper, which signs the salvage issue and commit.
- [docs/agent.md](agent.md) - the `ward agent` runs whose writes are attributed.
- [docs/FEATURES.md](FEATURES.md) - ward feature inventory.
