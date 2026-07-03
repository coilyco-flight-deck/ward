---
doc_goal: Make clear how a host operator overrides embedded fleet defaults without forking ward, by showing that the operator-local reader is a thin hook onto the one shared cli-guard fleetconfig validator, and pin down its precedence and fail-closed contract so a reader trusts what a present-but-wrong file does.
---
# fleet-local: the operator-local config reader

`~/.ward/fleet.local.kdl` is the **host-local** operator config: hand-edited,
git-ignored, and **never embedded** in a binary. [ward#413](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/413) wires the reader for
it ([aos#310](https://github.com/coilysiren/agentic-os/issues/310) §5, issue 10). The reader is a **shared-parser hook**, not a bespoke
`~/.ward` loader: it calls the one cli-guard `pkg/fleetconfig` validator under
the `OperatorLocal` source ([cli-guard#178](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/issues/178)), so ward never forks or re-implements
the fleet-config grammar.

## The file this reader reads

A minimal `~/.ward/fleet.local.kdl` is one `director` block. The reader accepts
only the narrow per-host node set, so this is close to the whole surface today:

    director {
        default-scope "coilyco-flight-deck"
    }

An operator hand-writes that file, git-ignores it, and ward reads it back as a
typed layer. The three terms the precedence section leans on:

- **OperatorLocal** - the cli-guard `fleetconfig` **source** the reader parses
  under, scoping the grammar to the per-host node subset (the `director` block)
  and rejecting the embed-only `fleet` block.
- **dialect-2** - the embedded fleet-config manifest (`ward-kdl.fleet.kdl`:
  identity, model, endpoint, attribution, roster defaults), compiled into the
  binary rather than read from disk. See [ward-kdl.md](ward-kdl.md).
- **the embed chain** - how those dialect-2 defaults get merged into the binary
  at build time via `fleetassets/`, the bottom layer of the precedence stack
  below.

## What the reader is

- [`cmd/ward/fleetlocal.go`](../cmd/ward/fleetlocal.go) resolves
  `~/.ward/fleet.local.kdl` (via `config.GlobalDir()` + the ward app dir), reads
  it, and hands the bytes to `fleetconfig.ParseSource(b, fleetconfig.OperatorLocal)`.
- It returns the parsed `fleetconfig.Fleet` as the typed operator-local layer ward
  code can read. Under `OperatorLocal` that Fleet carries only the narrow per-host
  node set (today the `director` block); the embed-only `fleet` block is rejected.

## Fail-closed, missing-is-empty

- A **missing** file is not an error. It yields a zero `fleetconfig.Fleet`, the
  empty layer an absent file should contribute, so an operator who never wrote one
  is not a failure.
- A **present-but-malformed** or **out-of-subset** file fails closed. A typo, or a
  smuggled embed-only `fleet` block, surfaces as an error rather than silently
  degrading to the empty layer and dropping the operator's intent.

## Precedence

The operator-local layer sits in the middle of the resolution chain:

- **env > operator-local > embedded manifest defaults.**

[ward#413](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/413) wires the **operator-local** layer only. The **embedded** layer merges in
with the dialect-2 embed chain ([aos#310](https://github.com/coilysiren/agentic-os/issues/310) issues 2-3), and **[ward#396](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/396)** files
`director.default-scope` as the first `OperatorLocal` field read against this
reader. This issue deliberately stops at the hook: it builds the reader and the
typed value, not the `director.default-scope` feature.

## See also

- [agent-director.md](agent-director.md) - the director scope resolution [ward#396](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/396) hangs on this reader.
- [agentsapi.md](agentsapi.md) - the sibling internal contract carved for the agent seam.
