# ward agent: `work`

`work` is the interactive carry-an-issue verb. It resolves a Forgejo issue,
trust-gates it, and launches a [`ward container`](container.md) seeded to take
the issue end to end. See [docs/agent.md](agent.md) for the verb family.

## What `work` does

1. **Resolve + validate.** Parses the ref, then fetches the issue from Forgejo so
   a bad ref, a typo, or an untrusted owner fails *before* any container spins.
2. **Trust-gate.** The target is refused unless its owner is in ward's primary-org
   set, because the container runs under `bypassPermissions`. A non-`open` issue
   warns but proceeds.
3. **Branch.** Derives `issue-<N>` as the feature branch (override with `--branch`).
4. **Launch.** Spins up an interactive `ward container` against a fresh clone of
   the repo, seeded with a "read the issue, then carry it to merge" prompt.

The seed prompt rides as the in-container agent's argv (the entrypoint's
`"$WARD_AGENT" "$@"`), so the agent opens already pointed at the issue. The
container doctrine
([AGENTS.container.md](../cmd/ward/containerassets/AGENTS.container.md)) supplies
the carry-to-merge autonomy and the reaper backstop; `work` only seeds the issue.

The seed's first move is shaped by the body ward already fetched at resolve time
and by the harness (ward#157):

- **Empty body, any harness.** The prompt says so outright ("this issue has no
  body, work from the title alone") rather than telling the agent to go read
  content that isn't there. An empty field plus a "go read it" instruction is
  what made qwen pattern-complete the gap with a fabricated screenshot URL,
  `read_image` it, and hard-kill the turn on a multimodal 400 against a
  non-vision ollama model.
- **Non-vision local harness (qwen/goose), real body.** The body is **inlined**
  into the seed with image markup (`![..](..)` embeds and bare image URLs)
  stripped, and the "read it at the URL" instruction is dropped - so the model
  has nothing to re-fetch or hallucinate around, and no screenshot to re-trip the
  multimodal 400. A body that is nothing but media collapses to the empty-body
  path above.
- **Vision-capable harness (claude/codex).** Keeps the original read-it-at-the-URL
  flow for non-empty bodies.

## Container name

Where a bare `container up` names its container `ward-<repo>-<rand>`, an agent
run names it for the work it carries: `ward-<repo>-issue-<N>-<mode>-<rand>`. So
`docker ps` reads the repo, the issue, and the harness at a glance, and a host
driving several agents at once can tell them apart - the `<rand>` suffix still
keeps concurrent runs on the same issue from colliding. `task` shows the shape
as `ward-<repo>-issue-<N>-<mode>-<rand>` under `--print`; the real number lands
once the issue is filed.

## See also

- [docs/agent.md](agent.md) - the `ward agent` verb family and usage.
- [docs/agent-subcommands.md](agent-subcommands.md) - `work` vs `headless`, `task`, `reply`, `ask`.
- [docs/container.md](container.md) - the container model `work` wraps.
