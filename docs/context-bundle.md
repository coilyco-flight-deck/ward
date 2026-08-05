---
doc_goal: Define Ward's provider-neutral, authority-free context-bundle contract for ephemeral agent containers.
---
# context-bundle handoff

Ward can launch an agent surface with one materialized generic context bundle:

```bash
warded engineer owner/repo#123 --context-bundle /path/to/bundle
```

The same contract supplies role context to a repository-free peer:

```bash
ward agent run --cluster codex-ab45 --harness codex --role critic --context-bundle /path/to/bundle "Review it."
```

The bundle is a directory with this shape:

```text
context-bundle.json
home/<selected instruction and skill root>
bin/<optional executable tools>
```

The strict manifest binds the context to the selected Ward role and agent:

```json
{
  "format": "ward.context-bundle.v1",
  "role": "engineer",
  "agent": "codex",
  "repositories": ["coilyco-flight-deck/agentic-os", "coilysiren/lore"]
}
```

Ward rejects unknown fields. A bundle cannot declare permissions, credentials,
network, source paths, or capabilities. `repositories` is required, nonempty,
strictly sorted, deduplicated, and limited to safe `owner/repository` identities.
See [repository references](context-bundle-repositories.md). Ward owns authority.

## Accepted home projection

Ward accepts only the selected agent's instruction file and skill root:

* `claude` - `.claude/CLAUDE.md` and `.claude/skills/`
* `codex` - `.codex/AGENTS.md` and `.agents/skills/`
* `goose` - `.config/goose/.goosehints` and `.agents/skills/`
* `opencode` - `.config/opencode/AGENTS.md` and `.agents/skills/`

Every bundle must provide the selected instruction file. Ward rejects other
paths, symlinks, special files, nested tools, and non-executable tools. Bootstrap
revalidates the read-only mount before copying accepted files to the agent home.

Ward keeps its authority document outside the immutable bundle. After bundle
projection, Ward appends it to the selected instruction load point. The agent
sees both selected context and the enforced authority boundary.

For a repository-free peer, repository targeting stays absent. Bundle and
substrate are read-only. Scratch, private homes, and runtime state are writable.

If `bin/` contains tools, Ward exposes the read-only directory after the image's
existing `PATH`. A bundled tool cannot shadow an image or harness binary.

On `warded director`, the bundle belongs to the director's own surface and is
not forwarded to engineers. Role-specific child bundles need their own host
launches. Ward refuses a bundle-backed nested dispatch because Docker cannot
preserve the parent container's host source as a read-only bind.

## Ownership boundary

* A producer owns context selection, skill generation, and materialization.
* Ward owns manifest validation, exact read-only mapping, private home projection,
  failure policy, credentials, permissions, network, filesystem authority, and teardown.
* Ward does not invoke or import a context producer.
* The bundle grants no command, credential, network, permission, or writable
  filesystem capability.
