# ward-kdl verb surface

The spec-driven and passthrough verb surfaces carried by `ward-kdl`, split out of
[FEATURES.md](FEATURES.md) under the documentation-layout cap (ward#224). For what
`ward-kdl` **is** - the build-time generator behind every surface below - see
[ward-kdl.md](ward-kdl.md).

## Spec-driven ops (`ward-kdl`)

- **`ward-kdl ops <api> <verb>`** - `specverb` API verbs: **forgejo** (Swagger 2.0, incl. `issue list-all`), **trello**/**tailscale** (OpenAPI 3.x). Denies teach. See [ops-forgejo](ops-forgejo.md).
- **`ward-kdl-{read,write,admin} ops forgejo <verb>`** - three permission-tiered forgejo binaries (ward#240), layered by `inherit` (cli-guard#160) over wildcard `"*"` grants (cli-guard#159): **read** = `get`/`list`, **write** adds `create`/`edit`, **admin** adds `delete`. Each tier is its own binary, so a withheld verb is absent at compile time, not denied at runtime. Distinct from the curated single-binary `ward-kdl ops forgejo` surface above. See [read](ward-kdl/ward-kdl.forgejo.read.guardfile.md), [write](ward-kdl/ward-kdl.forgejo.write.guardfile.md), [admin](ward-kdl/ward-kdl.forgejo.admin.guardfile.md).
- **`ward-kdl ops glitchtip <verb>`** - `specverb` GlitchTip (Sentry-compatible error tracking; OpenAPI 3.1, bearer, SSM-resolved opaque base-url). Org/issue/event reads, team + project + DSN CRUD, and the **`provision-project`** action that creates a project and mints its DSN in one shot for the bulk Sentry->GlitchTip cutover ([ward#170](ward-kdl/ward-kdl.glitchtip.guardfile.md)). See [glitchtip](ward-kdl/ward-kdl.glitchtip.guardfile.md).
- **`ward-kdl ops signoz <verb>`** - `specverb` SigNoz (traces + logs pane on ser8; hand-authored minimal OpenAPI, `SIGNOZ-API-KEY` static header-token, SSM-resolved opaque tailnet base-url). CRU only across `query-range`, log `pipeline`s, `dashboard`s, and alert `rule`s; every delete/destroy verb is denied and the enable/disable toggle stays human-in-the-UI (ward#241). See [signoz](ward-kdl/ward-kdl.signoz.guardfile.md).
- **`ward-kdl ops {aws,kubectl} <verb>`** - `execverb` local-CLI passthroughs: **aws** (SSM/S3/EC2 reads) and **kubectl** (reads + `diff` apply-preview + apply/scale/rollout; destructive verbs unexposed). See [aws](ward-kdl/ward-kdl.aws.guardfile.md), [kubectl](ward-kdl/ward-kdl.kubectl.guardfile.md).
- **`ward-kdl docker <verb>`** - `execverb` read-only Docker inspection (containers/images/volumes/networks, `logs`, `stats`, `inspect`, `events`); mutating + shell verbs unexposed, `exec` gated separately (ward#220). See [docker](ward-kdl/ward-kdl.docker.guardfile.md).
- **`ward-kdl agents <target> <verb>`** - mixed-transport. **`agents {claude,codex,opencode,aider,goose}`**: local-CLI launchers (`execverb`, `argv`-override). **`agents ollama`**: the tower's Ollama.
- **`ward-kdl pkg <resource> <verb>`** - `specverb` package-directory lookups: **skillsmp** (skills) and **glama** (Glama MCP), from `coily pkg` (ward#105); plus **`ward-kdl pkg brew <verb>`** - brew reads/passthrough (`execverb`, jailed; scoped verbs stay Go, [ward#95](ward-kdl.brew.scoped.md)). See [skillsmp](ward-kdl/ward-kdl.skillsmp.guardfile.md), [glama](ward-kdl/ward-kdl.glama.guardfile.md).

The **exec-dialect** surfaces above (`docker`, `agents`, `ops {aws,kubectl}`) are also auto-mounted into the `ward` binary at their own `wrap` path, so `ward docker ...` / `ward agents ...` / `ward ops aws ...` route to the same guarded surface (ward#284). `git` and `pkg brew` keep their hand-written `ward` surfaces. See [ward-kdl-in-ward](ward-kdl-in-ward.md).

## See also

- [ward-kdl.md](ward-kdl.md) - what `ward-kdl` is: the build-time generator behind these surfaces.
- [FEATURES.md](FEATURES.md) - inventory of what ships today.
- [ward-kdl-in-ward.md](ward-kdl-in-ward.md) - exec guardfiles auto-mounted into `ward`.
- [.ward/ward.yaml](../.ward/ward.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
