---
doc_goal: Give an operator the `--config` model-context override surface and the startup config echo end to end - the dotted-path grammar, the known keys and their WARD_* env targets, precedence, and where the resolved block prints - so a run's harness config is both steerable and visible.
---
# ward agent: `--config` model-context overrides

`--config` steers the launched agent's resolved model-context config from the CLI, and
every run now echoes that resolved config at container startup ([ward#616](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/616)).
Part of the [flag surface](agent-flags.md).

## The flag

`--config agent.<name>.<key>=<value>` is a **repeatable** dotted-path override. Each entry
rides into the container as the matching `WARD_*` env var, which the entrypoint's `envOr`
resolves **over the fleet default** - the same mechanism codex already read - so nothing
new is threaded per knob.

```
warded engineer #616 --config agent.claude.model=sonnet --config agent.claude.effort=medium
```

## Known keys

An unknown key **fails loud** at plan time, before any container spins, listing the full
set:

* `agent.claude.model` -> `WARD_CLAUDE_MODEL` (appended as `--model <v>` at launch)
* `agent.claude.effort` -> `WARD_CLAUDE_REASONING_EFFORT` (claude has no native effort flag today, so it is echoed in the startup config block, not applied to argv)
* `agent.codex.model` / `agent.codex.effort` / `agent.codex.verbosity` -> `WARD_CODEX_{MODEL,REASONING_EFFORT,VERBOSITY}`
* `agent.opencode.model` -> `WARD_QWEN_MODEL`, `agent.opencode.endpoint` -> `WARD_OLLAMA_URL`

## Precedence

Highest first: `--config` > `WARD_*` env > **per-role overlay** > flat per-agent fleet
default (the `agent <name>` node in [`ward-kdl.fleet.kdl`](ward-kdl.md)). With no `--config`,
no env, and no role overlay, today's behavior is unchanged - the claude launch omits
`--model` entirely. `--config` is approved on both the engineer and advisor dispatch broker
allowlists, so an in-container director surface can forward it host-side.

## Per-role model/effort defaults ([ward#620](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/620))

A role's launch config can diverge from the flat per-agent default with a per-agent
overlay on the role, using the same grammar the top-level `agent` node uses ([cli-guard#192](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/issues/192),
shipped in cli-guard v0.73.0):

```
role director {
    agent claude { model "claude-opus-4-8[1m]" reasoning-effort "high" }
}
```

The **director** claude resolves `claude-opus-4-8[1m]` (Opus 4.8, 1M context) at `high`
effort for its heartbeat; the **engineer** claude resolves the cheaper `claude-fable-5` at
`medium` for parallel fan-out (codex: director `gpt-5.5`, engineer `gpt-5.4-mini`).
`advisor` sets no overlay and inherits the flat default.

The overlay is authored in the fleet source, embedded via `make sync-fleet-assets`. The run's
config role rides in as `WARD_ROLE` (the **capability** role, so the director's read-only
surface resolves as `director` though it labels itself `session`), and the entrypoint's
`envOr` resolves each knob `env > overlay > flat default` - so `--config` / `WARD_*` still
wins a single steered run.

The `claude-opus-4-8[1m]` bracket form is a valid `--model` token (how 1M-context Opus is
selected), so no beta header is needed; claude effort stays echo-only.

## Startup config echo

Every warded run echoes the **resolved** model-context config for its agent at startup, in
one greppable block in the container log (`===== ward agent config =====`): agent name,
model, effort/reasoning, endpoint, and context-level. It fires for **every** agent, whether
or not that agent writes a config file - so a run's harness config is visible in the log
stream, not silent.

## See also

- [docs/agent-flags.md](agent-flags.md) - the full launch-flag surface.
- [docs/agent-claude.md](agent-claude.md) - claude's model/effort wiring.
