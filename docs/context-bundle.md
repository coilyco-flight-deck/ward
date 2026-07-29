---
doc_goal: Define Ward's provider-neutral, authority-free context-bundle contract for ephemeral agent containers.
---
# context-bundle handoff

Ward can launch an agent surface with one materialized generic context bundle:

```bash
warded engineer owner/repo#123 --context-bundle /path/to/bundle
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
  "agent": "codex"
}
```

Ward rejects unknown manifest fields. In particular, a bundle cannot declare
permissions, credentials, network access, mounts, or other capabilities. A
matching role selects context only. Ward's fixed launch and broker paths own
authority.

## Accepted home projection

Ward accepts only the selected agent's instruction file and skill root:

* `claude` - `.claude/CLAUDE.md` and `.claude/skills/`
* `codex` - `.codex/AGENTS.md` and `.agents/skills/`
* `goose` - `.config/goose/.goosehints` and `.agents/skills/`
* `opencode` - `.config/opencode/AGENTS.md` and `.agents/skills/`

Every bundle must provide the selected instruction file. Ward rejects other
home paths, symlinks, special files, nested tool directories, and non-executable
tools before Docker starts. Ward revalidates the read-only mount during
container bootstrap before copying the accepted home files into the private
agent home.

Ward keeps its authority document outside the immutable bundle. After the
bundle projection, Ward appends that authority document to the selected
instruction load point. The launched agent sees both its selected context and
the run's mechanically enforced authority boundary.

If `bin/` contains tools, Ward exposes the read-only directory after the image's
existing `PATH`. A bundled tool cannot shadow an image or harness binary.

On `warded director`, the bundle belongs to the director's own surface and is
not forwarded to engineers. Role-specific child bundles need their own host
launches. Ward refuses a bundle-backed nested dispatch because Docker cannot
preserve the parent container's host source as a read-only bind.

## Ownership boundary

* A producer owns context selection, skill generation, home materialization,
  and tool materialization.
* Ward owns its manifest schema, host validation, read-only Docker mount,
  selected role and agent checks, private home projection, launch failure
  policy, credentials, permissions, network, filesystem authority, and
  teardown.
* Ward does not invoke or import a context producer.
* The bundle grants no command, credential, network, permission, or writable
  filesystem capability.

Cross-repository composition on Ward's side is tracked in
[ward#1511](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1511).
