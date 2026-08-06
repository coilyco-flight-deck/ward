---
doc_goal: Define the complete provider-neutral, authority-free context bundle schema, projection, repository references, tools, and ownership boundary.
---
# Context bundle

`--context-bundle <directory>` adds one materialized read-only input to a
repository-backed role or repository-free peer.

```text
context-bundle.json
home/<selected instruction file and skill root>
bin/<optional executable tools>
```

The strict `ward.context-bundle.v1` manifest binds `role`, `agent`, and a
sorted, deduplicated, nonempty list of safe `owner/repository` identities.
Unknown fields and requests for permissions, credentials, network, source
paths, or capabilities are rejected.

## Home projection

Each harness accepts only its instruction load point and skill root. Ward
rejects other paths, symlinks, special files, nested tools, and non-executable
tools. Bootstrap revalidates the read-only mount before copying allowed files
into the private agent home. The bundle instruction remains authoritative.
Bootstrap composes it with Ward's minimal container authority and safety text
in memory, then atomically writes the selected harness's native instruction
file. It does not mutate the bundle, append to a projected file, create a
shared `~/AGENTS.md`, or write any sibling harness load point.

An optional `bin/` becomes `WARD_CONTEXT_TOOLS` after the image's existing
`PATH`, so it cannot shadow image or harness binaries.

## Repository references

For repository-backed launches, `$PROJECTS_ROOT` or the current checkout
selects the projects root. Each manifest identity must resolve to a real
directory at its exact owner-qualified path without symlinks or escape.
Validated checkouts mount read-only at `/refs/<owner>/<repository>`.

Repository-free peers retain bundle metadata but do not derive repository
mounts. Their scratch and private runtime homes are writable. Their bundle and
substrate remain read-only.

## Ownership

The producer owns context selection and materialization. Ward owns validation,
mapping, private-home projection, failure policy, credentials, permissions,
network, filesystem authority, and teardown. A director bundle belongs only
to that director and is not forwarded into nested engineers.

## See also

* [container-contract.md](container-contract.md) - mount and authority boundary.
* [agent-peer-collaboration.md](agent-peer-collaboration.md) - repository-free peers.
