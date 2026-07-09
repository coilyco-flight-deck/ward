---
doc_goal: Position ward against Dagger container-use - same weight class, both hand a coding agent a container - and show ward's distinct bet: a capability gate plus an autonomous issue-to-merge driver, versus blast-radius isolation with a human at merge.
---
# ward vs Dagger container-use

Written before someone else frames it. After NVIDIA OpenShell
([comparison-openshell.md](comparison-openshell.md)), container-use is the next
closest analog - and it shares ward's weight class exactly: a single Go binary,
Apache/FOSS, terminal-first. Both **hand the agent a container, not the
machine**. They diverge on **what bounds the box** and **who drives it home**.

## The two shapes

- **ward** - a single Go binary with two boundaries. The verb gate is
  **capability-level**: cli-guard compiles an OpenAPI spec plus a KDL guardfile
  into a scoped, audited CLI, so the agent can call `get` but never `delete`, and
  every call writes one append-only audit row. The container half (`ward agent`)
  then drives a coding agent **issue-to-merge autonomously** - implement, resolve
  conflicts, push `main` or open a PR.
- **container-use** (Dagger, Go, Apache-2.0, ~4k stars, early) - an MCP server
  that gives each agent **a fresh container on its own git branch**, powered by
  Dagger's engine. The boundary is **isolation**: parallel agents can't collide
  or wreck the host. A human stays in the driver seat - `cu watch` streams it and
  `cu merge` lands the chosen branch.

## Where the line falls

- **What the boundary is made of** - container-use bounds the **blast radius**:
  a bad step ruins a branch, not your tree, but *inside* the box the agent holds
  whatever the container holds and can call any API those credentials reach. ward
  bounds the **capability**: the compiled gate makes `delete` absent at compile
  time, so there is nothing to fail open. One contains what a mistake touches, the
  other removes what a call can be.
- **Who drives it home** - container-use keeps a **human at the merge**; ward's
  container half is a **headless driver** that pushes `main` itself.
  [ward#261](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/261)
  names the collision: the day container-use ships a credential-scoped headless
  driver mode is the day these niches meet. Today they do not.
- **Scope** - container-use bounds **coding agents only**. ward bounds those too,
  but *also* **cloud-agnostic ops verbs** (forgejo, aws, kubectl) through a typed
  least-privilege surface - ground container-use has no reach into.

## Why both can be right

Per-branch isolation with a human at the merge is the right tool when a person
fans several agents at a task and **chooses** the winning diff by hand. It is the
wrong tool for an operator who wants one audited `aws` call from a terminal, or a
run driven to `main` with a capability gate - not just isolation - holding the
line. That niche is ward's. Honesty cuts back: container-use's fan-out UX is more
polished than ward's v0.x coding container half, so where they overlap ward bets
on the *shape* of the boundary, not on out-polishing it.

## The boundary is the product

ward's claim is not "it can do X" - it is "it will refuse Y, and prove it": the
guardfile says `never delete "*"` and the binary cannot express the delete at
all, so nothing fails open and the audit row proves the crossing.
container-use bets from another layer - *isolation plus a reviewable diff*.
Both are real; ward's distinct bet is that the agent can't even name the
dangerous call. That claim covers the verb gate and container edge - see
[enforcement-boundary.md](enforcement-boundary.md).

## See also

- [comparison-openshell.md](comparison-openshell.md) - ward vs NVIDIA OpenShell.
- [README.md](../README.md) - what ward is.
- [dagger/container-use](https://github.com/dagger/container-use) - the project compared.
