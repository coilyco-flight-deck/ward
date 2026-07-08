---
doc_goal: Redirect anyone still reaching for qwen to the opencode harness, making clear qwen is a deprecated alias not a harness while explaining why the persona/model split keeps the Qwen signing identity, and route them to the real page.
---
# ward agent qwen (deprecated alias)

`qwen` is a **deprecated alias** for the `opencode` harness, not a harness of its
own. It is the pre-[ward#401](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/401) roster key: `--harness qwen` / `--mode qwen` still
resolves, but emits a deprecation warning and aliases straight to `opencode`. Use
`opencode` instead.

The split is deliberate: **`opencode` is the harness** (the local Ollama-backed
binary that self-installs at container start), while **`qwen` is the backing
model** - so the signing persona stays `Qwen` even though the mode is `opencode`
([agent-attribution.md](agent-attribution.md)).

The real page is **[agent-opencode.md](agent-opencode.md)**.

## See also

- [agent-opencode.md](agent-opencode.md) - the current `opencode` harness page (what `qwen` aliases to).
- [agent-local-harnesses.md](agent-local-harnesses.md) - the local Ollama-backed harness index.
- [agent-adapter-manifest.md](agent-adapter-manifest.md) - the roster entry, renamed from `qwen` by [ward#401](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/401).
