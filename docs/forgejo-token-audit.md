---
doc_goal: Enumerate every place ward reads the raw plain Forgejo bot token from the environment, classify each as a sanctioned chokepoint or a root-only plumbing site, and pin what remains to cut over - so "force everything through ward-kdl" has an auditable, test-enforced surface, not a scatter of `os.Getenv` reads.
---
# Raw Forgejo-token audit

**The plain `FORGEJO_TOKEN` is read only in a small, audited set of sites; every
other forge consumer routes through a resolver chokepoint - which tries the
[broker](broker.md) first - rather than pulling the raw token itself.** This is
the standing half of
[ward#239](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/239). The
full cutover that removes the token from the dropped agent's env is
[ward#608](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/608)
(Unit D), still blocked on a cli-guard **read op**. Until it lands the token must
stay present, so the enforceable win today is to **freeze the surface**.

## What "raw read" means

A raw read is `os.Getenv("FORGEJO_TOKEN")` (or the cli-guard constant
`os.Getenv(credseed.EnvForgejoToken)`) - the plain bot token straight from the
env. A consumer that instead calls a resolver (`forgejoTokenResolver`,
`resolveForgejoToken`, `gitForgejoAuthEnv`) holds no raw read and is the compliant
path, not an audited site.

## The audited sites

Five files read the raw token, in two classes.
`cmd/ward/forgejo_token_guard_test.go` pins exactly this set: a raw read in any
other file fails the build.

**Resolver chokepoints** - everything needing the token for a forge call funnels
through one of these; new code calls the resolver, never `os.Getenv`:

- **`ops.go` - `forgejoTokenResolver`** - the single auth provider for the
  `ward ops forgejo` specverb: baked `$FORGEJO_TOKEN` in a container, else the
  `coilyco-ops` bot token from SSM. `git_auth.go` and the network `ward git` verbs
  ride this same resolver, so they are compliant consumers, not separate sites.
- **`container.go` - `resolveForgejoToken`** - the host-side seed for a launched
  container's private `--env-file`. Tries the broker dispatch seed **first**, then
  env, then SSM - the chokepoint the broker's Unit C routing hangs off.

**Root-only plumbing** - runs as root before the privilege drop or after the agent
exits, where no resolver or broker socket is reachable and the raw token *is* the
credential. Correct end-state, not cutover debt:

- **`broker.go`** - the root credential daemon (`ward container broker`): it *is*
  the thing that holds the token so the dropped agent does not.
- **`container_bootstrap.go`** - the PID-1 entrypoint seeding
  `/etc/ward-git-credentials` before the drop. The push-token site tracked by
  [ward#161](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/161).
- **`container_reap.go`** - the deterministic reaper filing salvage issues and
  releasing the reservation on teardown, after the agent is gone.

## Still open

- **Unit D** drops the token from the dropped agent's env entirely. Blocked: forge
  **reads** still go direct and the broker has no read op yet, so scrubbing first
  would blind explore sessions (see [broker.md](broker.md) "Dual mode").
- **[ward#161](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/161)**
  - the in-container git push-token site, folded into the same cutover.

The audit makes no code change to those today - its job is to keep the surface
from growing while they land.

## See also

- [broker.md](broker.md) - the root credential broker the resolvers route through.
- [agent-credentials.md](agent-credentials.md) - how each harness credential is seeded.
- `cmd/ward/forgejo_token_guard_test.go` - the build-time enforcement of this audit.
