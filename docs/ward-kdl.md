---
doc_goal: One answer to what ward-kdl is, how it differs from ward, and whether a repo needs a guardfile - build-time authoring layer vs run-time product.
---
# ward-kdl: the build-time authoring layer

**ward-kdl is the build-time authoring layer. `ward` is the run-time product that embeds what it authors. [cli-guard](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard) is the engine both stand on.** Three roles, told apart by **when** they run. The conceptual model lives in [architecture.md](architecture.md).

## Do you need to author a guardfile? (start here)

Almost certainly not. **A repo needs a guardfile only when it runs its own `ward
ops` operator surface. Dev-verb adoption is `.ward/ward.yaml` and nothing else.**
The fork every adopter hits first:

- **Dev-verb gate** (`ward exec build/test/lint`, `ward git`, the audit log) on your repo: a committed [`.ward/ward.yaml`](../.ward/ward.yaml) is the whole contract, no guardfile. That surface is forge-agnostic and already embedded in the installed `ward`. See [ward-yaml.md](ward-yaml.md).
- **Your own operator surface** (`ward ops forgejo/aws`, the fleet roster) against your own endpoints, tokens, and owner gate: only then a guardfile, and even then you **swap placeholders** in the neutral example bundle and rebuild from source. See [ward-kdl-authoring.md](ward-kdl-authoring.md) and the grammar in [guardfile-grammar.md](guardfile-grammar.md). Guardfiles are ward's own build-time internals, not a per-repo file a `ward exec` user maintains.

## Build time: source in, validated artifact out

You author a source file, cli-guard compiles or validates it, `ward` embeds the
result. Nothing is fetched at runtime. `ward-kdl` is `protoc` for permissions and
fleet config, and you rarely run it by hand: you run what it produced and
regenerate when the source changes (`make build-ward-kdl`). Three dialects:

- **Dialect 1, permission surfaces** - `*.guardfile.kdl` spec + exec files. Least-privilege, audited. Parsed by `cli/execverb` + `http/specverb`.
- **Dialect 2, fleet-config manifest** - `ward-kdl.fleet.kdl`: identity, model, endpoint, attribution, roster defaults, sparse overrides, `roles`.
- **Dialect 3, operator-local** - the same `fleetconfig` parser, sourced from a local `~/.ward/fleet.local.kdl`, not embedded and tracked separately.
- **Smart defaults bundle** - `ward-kdl.defaults.kdl`: selected runtime policy defaults plus repo-authority for trusted-owner and bare-ref resolution. Parsed via `defaultsassets/`.

## Run time: `ward` embeds the emitted surfaces

`ward` (public face `warded`) is the product a user installs. It embeds the ward-kdl surfaces as `ward ops <api>`, `ward docker`, `ward agents <target>`, then adds the run-time-only layers ward-kdl never produces: `ward agent` and `ward exec`. The embeds are the baked default. `WARD_CONFIG_REF` still swaps edge-mounted surfaces at launch, but core agent/container defaults stay ward-owned ([config-source.md](config-source.md)). Exec guardfiles auto-mount at their `wrap` path ([ward-kdl-in-ward.md](ward-kdl-in-ward.md)).

## The per-area reference output

`ward-kdl` can still emit per-area Markdown reference output beside each guardfile, but that output is generated material, not release-era docs, and this repo no longer commits it. [ward-kdl-surface.md](ward-kdl-surface.md) is the committed flat index across every area.

## See also

- [guardfile-grammar.md](guardfile-grammar.md) - the dialect-1 KDL grammar and a minimal guardfile.
- [ward-kdl-authoring.md](ward-kdl-authoring.md) - getting the compiler, swapping the bundle.
- [architecture.md](architecture.md) - the three-layer model (cli-guard / ward-kdl / ward).
- [ward-kdl-surface.md](ward-kdl-surface.md) - the full generated verb surface, area by area.
- [ward-kdl-in-ward.md](ward-kdl-in-ward.md) - how exec guardfiles auto-mount into `ward`.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
