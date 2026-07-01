---
doc_goal: Make a new contributor grasp ward as a deliberate containment system where the boundary is the product, not a tidy permission wrapper - and feel why the cli-guard / ward-kdl / ward split is load-bearing rather than incidental.
---
# Architecture: ward in three layers

ward is a permissions, policy, and audit layer for headless agents that hold real credentials and act on live infrastructure. Three pieces make that work, and they're easy to tell apart by their **job** - and, crucially, by **when** they run.

## cli-guard - the engine

A Go library of the actual guardrails: deny-by-structure command lockdown, the OpenAPI→audited-verb engine (`specverb`), passthrough dialects (`execverb`), and the ephemeral-container sandbox. It holds no opinions about *your* APIs - it is the reusable enforcement core. You could build your own tool on it.

## ward-kdl - the build-time authoring layer

You author a source file, cli-guard validates or compiles it, and `ward`
embeds the result. `ward-kdl` spans three dialects: permission surfaces
(`*.guardfile.kdl`), fleet-config manifests (`ward-kdl.fleet.kdl`), and the
operator-local `~/.ward/fleet.local.kdl` boundary. Source in, artifact out,
nothing fetched at runtime. [ward-kdl.md](ward-kdl.md) is the authoritative
writeup of this layer.

## ward (public face: `warded`) - the product (run time)

The CLI you actually run. It **embeds** the ward-kdl-generated surfaces (`ward ops <api>`) and adds the agent layer (`ward agent` - drive a headless harness inside a guarded container) and guarded dev commands (`ward exec`). This is the thing a user installs and the thing the launch is about.

## In one line

**cli-guard** enforces, **ward-kdl** compiles policy into least-privilege CLIs, **ward** (`warded`) is the agent-facing product you run.

## The boundary this implies

The model only stays true if the boundary it claims is real: the only Go in **ward** is the run-time product (agent + exec + the embedding); everything reusable-and-policy lives in **cli-guard**; everything guardfile→binary lives in **ward-kdl**. Where that boundary is still blurry today is tracked as cleanup - the articulation comes first, the code catches up.

## Considered and rejected: folding cli-guard into ward

Release friction - keeping `cli-guard` and `ward` in step across a module
boundary - has more than once prompted "why not merge cli-guard into ward and be
done with the boundary?" It was considered and **rejected** (ward#325). The
boundary is load-bearing; collapsing it inverts the architecture.

cli-guard is the engine *both* other layers stand on:

- **ward** (run time) imports cli-guard as a library - ~23 packages of it.
- **ward-kdl** (build time) *is* the build-time authoring layer: it writes the
  guardfile or fleet manifest that cli-guard validates and ward embeds.

Fold cli-guard into ward and ward-kdl's dependency inverts: the build-time
generator would have to depend on the run-time product to reach the engine -
a compiler depending on the application it compiles. ward is also where the
live-credential, agent-facing surface lives, so the merge would drag
credential-bearing run-time code into the build step whose whole job is to emit
*least-privilege* surfaces. Backwards on both counts.

The merge also erases the one thing the three-layer split exists to give: an
**independently auditable enforcement boundary**. cli-guard "holds no opinions
about *your* APIs" precisely because it is a separate, reusable core someone
else could build on. Dissolve it into ward and the enforcement core is no longer
separable from the product that uses it - there is nothing left to audit on its
own.

The release friction is real, but the fix is a **Go workspace** (`go.work`) that
lets the inner dev loop resolve cli-guard locally without version churn - the
boundary stays, only dev-time resolution gets easier. Collapsing the module
boundary to save a few release steps trades a load-bearing wall for a
convenience.
