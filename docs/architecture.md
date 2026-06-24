# Architecture: ward in three layers

ward is a permissions, policy, and audit layer for headless agents that hold real credentials and act on live infrastructure. Three pieces make that work, and they're easy to tell apart by their **job** - and, crucially, by **when** they run.

## cli-guard - the engine

A Go library of the actual guardrails: deny-by-structure command lockdown, the OpenAPI→audited-verb engine (`specverb`), passthrough dialects (`execverb`), and the ephemeral-container sandbox. It holds no opinions about *your* APIs - it is the reusable enforcement core. You could build your own tool on it.

## ward-kdl - the generator (build time)

You write a **guardfile** - a small [KDL](https://kdl.dev) policy file: for an API, `can get "*"`, `never delete "*"`. `ward-kdl` compiles that guardfile plus the API's OpenAPI spec into a **scoped, audited CLI binary**. It is `protoc` for permissions: a grammar (the guardfile) in, a typed least-privilege surface out. You rarely run `ward-kdl` by hand - you run what it produces.

## ward (public face: `warded`) - the product (run time)

The CLI you actually run. It **embeds** the ward-kdl-generated surfaces (`ward ops <api>`) and adds the agent layer (`ward agent` - drive a headless harness inside a guarded container) and guarded dev commands (`ward exec`). This is the thing a user installs and the thing the launch is about.

## In one line

**cli-guard** enforces, **ward-kdl** compiles policy into least-privilege CLIs, **ward** (`warded`) is the agent-facing product you run.

## The boundary this implies

The model only stays true if the boundary it claims is real: the only Go in **ward** is the run-time product (agent + exec + the embedding); everything reusable-and-policy lives in **cli-guard**; everything guardfile→binary lives in **ward-kdl**. Where that boundary is still blurry today is tracked as cleanup - the articulation comes first, the code catches up.
