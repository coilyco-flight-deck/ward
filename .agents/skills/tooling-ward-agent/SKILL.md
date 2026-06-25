---
name: tooling-ward-agent
description: Normalize a dictated ward agent phrase to owner/repo#N and dispatch the engineer carry (detached, or --watch). Triggers - ward agent, dispatch, fire an agent, spawn an agent, fan out.
---

# tooling-ward-agent

`ward agent <role> <ref>` is a privileged op: it spins an ephemeral container that fresh-clones the repo and carries a Forgejo issue to merge under `bypassPermissions`. Mis-parsing a dictated ref silently sends an agent at the wrong issue. This skill normalizes a dictated reference into a canonical `owner/repo#N` and dispatches the engineer carry (successor to `ward dispatch`/`ward drive`; ward#174, ward#282; roster rename ward#347). Canonical in `coilyco-flight-deck/ward` (ward#286).

## Assumptions

Fan-out happens *before* this skill (`writing-to-issues`/`tooling-sidequest` sliced the work and filed the issues). This skill takes one dictated reference to one already-open issue, resolves it, dispatches the engineer carry, hands off - it does not slice work or create issues.

## When to fire

Any user phrase containing "dispatch", "agent", or "spawn" plus a numeric tail. Also "fan out", "run claude on", and interactive-intent phrasing ("open one for me", "spin this up", "let me iterate on this", "HITL this") paired with an issue.

Do NOT fire when the user already typed a clean `owner/repo#N` or a Forgejo issue URL - pass straight through to `ward agent`. A bare `#N` from inside a repo checkout also passes straight through: `ward agent` infers `owner/repo` from the cwd's git origin (ward#282).

## Step 1: refresh the registry

`data/repo-registry.md` in `coilyco-bridge/agentic-os-kai` carries each active repo's canonical `owner/repo`, regenerated daily (`sync-repo-registry.yml`), so the local checkout lags. Read the live copy before resolving:

```bash
gh api repos/coilyco-bridge/agentic-os-kai/contents/data/repo-registry.md --jq '.content' | base64 -d
```

If `gh` is unreachable, fall back to local with a one-line caveat to Kai.

## Step 2: resolve the ref

Lowercase the repo tokens, strip hyphens/spaces, fuzzy-match the registry's repo column, and take the **owner from the matched row** - repos span four orgs, so the owner is per-repo, not a fixed default. The full filler list and the baked-in voice-collision table (e.g. "coily" alone -> `coilyco-bridge/coily`, "coily co ai" -> `coilyco-bridge/agentic-os-kai`) live in [`references/normalization.md`](references/normalization.md). Use it, do not guess.

## Step 3: confirm, or refuse and explain

Confirm one line with the issue title from the Forgejo API (`ward ops forgejo issue view <owner> <repo> <N>`, or `gh issue view <ref>` for a mirrored GitHub ref):

> Resolved: `coilyco-bridge/coily#125` - "<title>". Send an agent?

Skip confirmation only on a unique, unambiguous match; ALWAYS confirm when two repos fuzzy-match. Refuse (naming the failing condition) if the issue is closed, the owner is outside the four-fleet-org trust set (see `references/normalization.md`), the repo did not resolve, or the lookup errors.

## Step 4: dispatch the engineer carry

`ward agent <role> <ref>` takes a role (`engineer`|`architect`|`director`|`advisor`; ward#347) and `--driver` picks the harness (default claude). This skill dispatches **`engineer`** - implement a ticket end to end. A **bare ref runs the engineer carry** (detached, fire-and-forget; the PR is the review gate); `--watch` (`-w`) attaches it when Kai signals supervision. Explicit words win. Heuristics + examples: [`references/surfaces.md`](references/surfaces.md).

```bash
ward agent engineer coilyco-flight-deck/<repo>#<N>           # detached, fire-and-forget
ward agent engineer coilyco-flight-deck/<repo>#<N> --watch   # attached, pair-with-me
```

## Out of scope

* Container model, clone, seeding, reservation, reaper, audit (owned by `ward agent`).
* Authoring the issue body (Kai's job); slicing work into issues (`writing-to-issues`, `tooling-sidequest`).
