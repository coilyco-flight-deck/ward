# ward agent: interactive pre-launch gate (ward#366)

The seedless interactive bring-up (`runScratchSession`, the director's read-only
surface - both its drain-surface and its init-gate "scope now" path; ward#353)
resolved a visible
chunk of state - repo, mode, access, image, ward version, `--repo` grants -
then **immediately** `docker run` into the fullscreen agent TUI. That metadata
scrolled past the instant the alt-screen took the terminal, with nowhere to act
on it (notably, no chance to refresh a stale `ward` first). The **pre-launch
gate** is the status-comms area that launch lacked: it sits right before
`createAgentContainer` fires the interactive run.

## Affordance A: Enter to launch

When a terminal is attached and this is not `--print`, the gate renders a compact
status block - the facts `printScratchPlan` knows - and **blocks on Enter** before
the TUI launches:

```
── ward pre-launch ─────────────────────────────────
  access:   read-only
  repo:     coilyco-flight-deck/ward
  agent:    claude (claude)
  image:    .../agentic-os:latest
  ward:     v0.16.0
  with:     coilyco-flight-deck/cli-guard
────────────────────────────────────────────────────
Press Enter to launch.
```

A readable pre-flight summary and a deliberate "go" instead of being teleported
into the alt-screen. For `director`'s drain-surface this doubles as the "new
direction" comms area before the read-only session opens.

## Affordance B: u to upgrade ward, then re-launch

When the host `ward` is behind latest (the `version.Behind` read that drives the
dispatch heads-up, ward#143), the gate offers a second choice - type `u` then
Enter to run `ward upgrade` and **re-launch the same invocation**:

```
host ward v0.16.0 is behind the latest release v0.17.0.
Press Enter to launch, or type u then Enter to upgrade ward and re-launch.
```

This retires the operator's manual `watch "brew upgrade ..."` loop.

After `ward upgrade` the on-disk binary is new but the running process is the old
`ward`, so re-launch re-execs the freshly-installed binary with the current argv
(`syscall.Exec`), not stale in-memory code. The exec target is canonicalized
against the same homebrew allow-list the PreToolUse hook uses
(`guardBinaryPaths["ward"]`), so it can't be PATH-hijacked. With no canonical path
(a dev/source build) or a failed exec, it falls back to the v1: report the upgrade
and tell the operator to re-run, launching nothing stale.

## When the gate is skipped

- **`--print`** returns the dry-run plan before the gate is reached.
- **A non-TTY stdin** (headless, detached, piped) falls **straight through** - it
  is never blocked on an Enter that would never come. The gate consults
  `terminalAttached()`; when false it keeps the stale-ward heads-up and proceeds.

Engineer is detached-only (ward#356), so it has no TUI and the gate never applies.

## Seams

The stdin read, TTY probe, and re-exec sit behind package seams
(`gateTerminalAttached`, the `Runner`'s reader/writer, `reExec`) so tests drive the
status block, the outdated branch, and gate-shown-vs-skipped without a terminal.
See `cmd/ward/agent_gate.go`.

## See also

- [docs/agent-surface.md](agent-surface.md) - the read-only surface the gate fronts.
- [docs/agent-preflight.md](agent-preflight.md) - the detached GO/NO-GO pre-flight.
- [docs/hook.md](hook.md) - the path-canonicalization the re-exec mirrors.
