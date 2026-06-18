# Unknown-verb fallback + the build/test/install triple

## Unknown-verb fallback

`ward <leaf>` is rewritten to `ward exec <leaf>` when `<leaf>` is **not** a
registered top-level verb (`version, upgrade, exec, pkg, git, audit, doctor,
hook, install-hooks, container, agent`, plus `help`) and **does**
match a command declared in the reachable `.ward/ward.yaml`. So `ward test`
just works wherever `test` is declared, without each repo leaf being
hardcoded as a top-level command (which would duplicate dispatch and risk
shadowing real verbs).

The rewrite is conservative:

- Registered top-level verbs always win - a repo leaf named `git` never
  shadows the `ward git` verb. The verb set is read from the live command
  list, so new verbs are covered automatically.
- A genuinely unknown verb with no matching leaf (`ward bogus`) is left
  untouched, so cli's own command-not-found still fires cleanly (non-zero).
- Root flags and their values (`ward --config x.yaml test`,
  `ward --audit-override-dirty test`) are carried through ahead of the
  spliced `exec`, and any leaf args after the verb are preserved.
- The config is only loaded for verbs that are not already top-level, so
  normal invocations pay nothing.

The rewritten argv re-enters the same verb pipeline as an explicit
`ward exec <leaf>`: shell-metacharacter policy, the audit row, and the
[clean-tree gate](exec-verb.md) all still apply.

## The build/test/install triple

Every ward-managed repo is expected to declare three commands -
**`build`**, **`test`**, and **`install`** - each backed by a Makefile
target. These are exactly what a headless agent needs to bootstrap an
unfamiliar repo blind: compile it, prove it, and put the artifact where it
runs. With the fallback above, `ward build` / `ward test` / `ward install`
then work bare in any compliant repo with no per-repo wiring.

The triple is a *contract*, not three hardcoded commands: dispatch is already
handled by the fallback, so blessing it means enforcing the declarations (a
fleet-rolled `ward-verb-triple` pre-commit linter in
`coilyco-flight-deck/agentic-os`) rather than adding Go commands here.
`install` is deliberately **not** standardized across repos - a Go CLI uses
`go install`, Python uses `uv sync`, JavaScript uses `npm ci`, docs repos use
a no-op - so only the *name* and the backing target are required, not the
implementation. This repo's own `install` runs `go install ./...`.

Introduced in
[ward#87](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/87).

## See also

- [docs/exec-verb.md](exec-verb.md) - the `exec` verb and clean-tree gate.
- [docs/FEATURES.md](FEATURES.md) - inventory of what ships.
