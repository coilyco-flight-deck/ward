# ward-kdl verb surface

The spec-driven and passthrough verb surfaces carried by `ward-kdl`, split out of
[FEATURES.md](FEATURES.md) under the documentation-layout cap (ward#224).

## Spec-driven ops (`ward-kdl`)

- **`ward-kdl ops <api> <verb>`** - `specverb` API verbs: **forgejo** (Swagger 2.0, incl. `issue list-all`), **trello**/**tailscale** (OpenAPI 3.x). Denies teach. See [ops-forgejo](ops-forgejo.md).
- **`ward-kdl ops glitchtip <verb>`** - `specverb` GlitchTip (Sentry-compatible error tracking; OpenAPI 3.1, bearer, SSM-resolved opaque base-url). Org/issue/event reads, team + project + DSN CRUD, and the **`provision-project`** action that creates a project and mints its DSN in one shot for the bulk Sentry->GlitchTip cutover ([ward#170](ward-kdl.glitchtip.guardfile.md)). See [glitchtip](ward-kdl.glitchtip.guardfile.md).
- **`ward-kdl ops {aws,kubectl} <verb>`** - `execverb` local-CLI passthroughs: **aws** (SSM/S3/EC2 reads) and **kubectl** (reads + apply/scale/rollout; destructive verbs unexposed). See [aws](ward-kdl.aws.guardfile.md), [kubectl](ward-kdl.kubectl.guardfile.md).
- **`ward-kdl docker <verb>`** - `execverb` read-only Docker inspection (containers/images/volumes/networks, `logs`, `stats`, `inspect`, `events`); mutating + shell verbs unexposed, `exec` gated separately (ward#220). See [docker](ward-kdl.docker.guardfile.md).
- **`ward-kdl agents <target> <verb>`** - mixed-transport. **`agents {claude,codex,opencode,aider,goose}`**: local-CLI launchers (`execverb`, `argv`-override). **`agents ollama`**: the tower's Ollama.
- **`ward-kdl pkg <resource> <verb>`** - `specverb` package-directory lookups: **skillsmp** (skills) and **glama** (Glama MCP), from `coily pkg` (ward#105); plus **`ward-kdl pkg brew <verb>`** - brew reads/passthrough (`execverb`, jailed; scoped verbs stay Go, [ward#95](ward-kdl.brew.scoped.md)). See [skillsmp](ward-kdl.skillsmp.guardfile.md), [glama](ward-kdl.glama.guardfile.md).

## See also

- [FEATURES.md](FEATURES.md) - inventory of what ships today.
- [.ward/ward.yaml](../.ward/ward.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
