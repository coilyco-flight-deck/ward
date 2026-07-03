---
doc_goal: Give an operator the full map of audited `ward-kdl` verb surfaces - the spec-driven ops APIs, the permission-tiered forgejo binaries, and the exec-dialect passthroughs auto-mounted into `ward` - so they can find the right guarded verb and know which transport and tier backs it.
---
# ward-kdl verb surface

Every surface below is a **least-privilege audited verb compiled from a
guardfile** - the guardfile declares exactly which operations exist, cli-guard
generates the CLI from it, and the compiled artifact is the boundary, not a
hand-curated command list. Read this as ward's ops security surface, not a menu.

Two verb kinds tag the whole catalog:

- **`specverb`** - spec-driven REST. A pruned OpenAPI/Swagger lock plus a
  guardfile generate typed API verbs (forgejo, glitchtip, signoz, ...). Only the
  granted operations compile in.
- **`execverb`** - local-CLI passthrough. A guardfile wraps an existing binary
  (aws, kubectl, docker, ...), exposing only the allowed argv shapes and jailing
  the rest.

The spec-driven and passthrough verb surfaces carried by `ward-kdl`, split out of
[FEATURES.md](FEATURES.md). For what `ward-kdl` **is** - the build-time
authoring layer behind every surface below - see [ward-kdl.md](ward-kdl.md).

## Spec-driven ops (`ward-kdl`)

- **`ward-kdl ops <api> <verb>`** - `specverb` API verbs: **forgejo** (Swagger 2.0, incl. `issue list-all`), **trello**/**tailscale** (OpenAPI 3.x). Denies teach. See [ops-forgejo](ops-forgejo.md).
- **`ward-kdl-{read,write,admin} ops forgejo <verb>`** - three permission-tiered forgejo binaries, layered by `inherit` over wildcard `"*"` grants: **read** = `get`/`list`, **write** adds `create`/`edit`, **admin** adds `delete`. Each tier is its own binary, so a withheld verb is absent at compile time, not denied at runtime. Distinct from the single-binary `ward-kdl ops forgejo` above. See [read](ward-kdl/ward-kdl.forgejo.read.guardfile.md), [write](ward-kdl/ward-kdl.forgejo.write.guardfile.md), [admin](ward-kdl/ward-kdl.forgejo.admin.guardfile.md).
- **`ward-kdl ops glitchtip <verb>`** - `specverb` GlitchTip (Sentry-compatible; OpenAPI 3.1, bearer, SSM base-url). Org/issue/event reads, team + project + DSN CRUD, and the **`provision-project`** action that creates a project + mints its DSN (bulk Sentry->GlitchTip cutover, [ward#170](ward-kdl/ward-kdl.glitchtip.guardfile.md)). See [glitchtip](ward-kdl/ward-kdl.glitchtip.guardfile.md).
- **`ward-kdl ops signoz <verb>`** - `specverb` SigNoz (traces + logs pane on ser8; minimal OpenAPI, `SIGNOZ-API-KEY` header-token, SSM tailnet base-url). CRU only across `query-range`, log `pipeline`s, `dashboard`s, and alert `rule`s; deletes/destroys denied, enable/disable stays in-UI. See [signoz](ward-kdl/ward-kdl.signoz.guardfile.md).
- **`ward-kdl ops {aws,kubectl,forgejo-key} <verb>`** - `execverb` local-CLI passthroughs: **aws** (SSM/S3/EC2 reads), **kubectl** (reads + `diff` apply-preview + apply/scale/rollout), **forgejo-key** (a sealed single-key reader for the Forgejo token, the non-2FA SSM bypass). See [aws](ward-kdl/ward-kdl.aws.guardfile.md), [kubectl](ward-kdl/ward-kdl.kubectl.guardfile.md), [forgejo-key](ward-kdl/ward-kdl.forgejo-key.guardfile.md).
- **`ward-kdl docker <verb>`** - `execverb` read-only Docker inspection (containers/images/volumes/networks, `logs`, `stats`, `inspect`, `events`); mutating + shell verbs unexposed, `exec` gated separately. See [docker](ward-kdl/ward-kdl.docker.guardfile.md).
- **`ward-kdl agents <target> <verb>`** - mixed-transport. **`agents {claude,codex,opencode,aider,goose}`**: local-CLI launchers (`execverb`, `argv`-override). **`agents ollama`**: the tower's Ollama.
- **`ward-kdl pkg <resource> <verb>`** - `specverb` package-directory lookups: **skillsmp** (skills) and **glama** (Glama MCP), from `coily pkg`; plus **`ward-kdl pkg brew <verb>`** - brew reads/passthrough (`execverb`, jailed; scoped verbs stay Go, [ward#95](ward-kdl.brew.scoped.md)). See [skillsmp](ward-kdl/ward-kdl.skillsmp.guardfile.md), [glama](ward-kdl/ward-kdl.glama.guardfile.md).

The **exec-dialect** surfaces above (`docker`, `agents`, `ops {aws,kubectl}`) are also auto-mounted into the `ward` binary at their own `wrap` path, so `ward docker ...` / `ward agents ...` / `ward ops aws ...` route to the same guarded surface. `git` and `pkg brew` keep their hand-written `ward` surfaces. See [ward-kdl-in-ward](ward-kdl-in-ward.md).

## See also

- [ward-kdl.md](ward-kdl.md) - what `ward-kdl` is: the build-time authoring layer behind surfaces.
- [FEATURES.md](FEATURES.md) - inventory of what ships today.
- [ward-kdl-in-ward.md](ward-kdl-in-ward.md) - exec guardfiles auto-mounted into `ward`.
- [.ward/ward.yaml](../.ward/ward.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
