# `ward pkg brew` scoped verbs - why they stay gated Go (ward#95)

[ward#94](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/94)
re-expressed the read/passthrough half of `ward pkg brew`
(`cmd/ward/pkg_brew.go`) as the exec-dialect
[`ward-kdl.brew.guardfile.kdl`](../cmd/ward-kdl/ward-kdl.brew.guardfile.kdl).
[ward#95](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/95)
asked whether the scoped/mutating half fits the exec dialect or needs a complex
action. (`ward-kdl.brew.guardfile.md` is `specverb-gen` output, so the rationale
lives here.)

## The scoped half

All three classes key off the primary-org taps with one `--allow-untapped`
opt-out: **formula-scoped** (`install`/`uninstall`/`upgrade`/`reinstall`/`link`/
`unlink`/`pin`/`unpin`) needs each formula in tap scope (`brewInTapScope`);
**tap-scoped** (`tap`/`untap`) needs each positional under a primary-org tap;
**touch-everything** (`cleanup`/`autoremove`/`services cleanup`) needs
`--allow-untapped` outright.

## Decision

**Fits neither the exec dialect's guards nor a complex action at cli-guard
v0.32.0 - stays hand-written gated Go (`cmd/ward/pkg_brew.go`).** The guardfile
keeps only the read/passthrough half and marks every scoped verb `never run`.
Mirrors ward#92's "`forgejo_issue.go` stays load-bearing"
([ops-forgejo-in-ward.md](ops-forgejo-in-ward.md)).

### Guards can't express it

The dialect's only argv guards are `when`/`deny-when <selector> matches
<glob...>` (`cli/execverb/execverb.go`):

1. **∃ vs ∀.** `when` passes on *one* match (`firstMatch` is existential); the
   rule denies if *any* formula is out of scope. `brew install ward evilpkg`
   satisfies `when any-arg matches "coilysiren/*/*" "ward"` yet must be denied.
   `deny-when` can't invert it - it needs a glob for "out-of-scope", the negation
   of the allow set, which the matcher (`*`=any substring, no negation) can't write.
2. **No conditional escape.** "Deny unless in-scope **or** `--allow-untapped`":
   guards are unconditional and AND-ed, with no "skip when a flag is present".
3. **Flag can't be consumed.** `actionFor` forwards every arg verbatim;
   `--allow-untapped` is ward-side (`splitBrewArgs` pops it) and would leak to
   brew as an unknown flag.
4. **Zero-positional gate.** Bare `brew upgrade` (no formula) must be denied, but
   guards react only to *selected values*.
5. **Touch-everything inversion.** "require `--allow-untapped` present" is the
   inverse of deny-on-match, and (3) still leaks the flag.

### A complex action isn't reachable from ward

The only extension seam is the gate registry (`gateRegistry`), a closed map with
one `aws-read` gate. `execverb.Config` exposes `Providers` but no
gate-injection field, so ward can't register a `brew-scope` gate the way
`specverb` names complex actions (`ci-watch`, `move-issue`). Needs an upstream
cli-guard change.

## What would unblock it

A `Config.Gates` injection field (or a first-class `brew-scope` gate in
`gateRegistry`) **plus** a flag-stripping seam so `--allow-untapped` is consumed
before exec. Then the scope check moves out of Go and the `never run` markers
flip to `can run … { gate brew-scope }`. Filing that upstream is the follow-up.

## See also

- [ops-forgejo-in-ward.md](ops-forgejo-in-ward.md) - the parallel ward#92 decision.
- [exec-verb.md](exec-verb.md) - the exec dialect overview.
